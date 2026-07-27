package hosts

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chinmay28/deployer/server/internal/sshx"
	"github.com/chinmay28/deployer/server/internal/store"
)

// These tests drive the real SSH path against a throwaway sshd on localhost:
// key auth, host-key pinning, and the /proc probe against a live kernel. They
// skip where sshd is unavailable rather than silently passing.
func requireSSHD(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"/usr/sbin/sshd", "ssh-keygen"} {
		if _, err := exec.LookPath(bin); err != nil {
			if _, statErr := os.Stat(bin); statErr != nil {
				t.Skipf("%s not available: %v", bin, err)
			}
		}
	}
	if os.Geteuid() != 0 {
		t.Skip("sshd test server needs root")
	}
}

// startSSHD boots an sshd that accepts only the given authorized key and
// returns the port it listens on.
func startSSHD(t *testing.T, authorizedKey string) int {
	t.Helper()
	dir := t.TempDir()
	// sshd rejects world-readable key material regardless of StrictModes.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	hostKey := filepath.Join(dir, "host_ed25519")
	kg := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", hostKey)
	if out, err := kg.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}

	authFile := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(authFile, []byte(authorizedKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	cfg := filepath.Join(dir, "sshd_config")
	conf := fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s
PidFile %s
AuthorizedKeysFile %s
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin prohibit-password
StrictModes no
UsePAM no
PrintMotd no
LogLevel ERROR
`, port, hostKey, filepath.Join(dir, "sshd.pid"), authFile)
	if err := os.WriteFile(cfg, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	// sshd refuses to start without its privilege separation directory.
	if err := os.MkdirAll("/run/sshd", 0o755); err != nil {
		t.Skipf("cannot create sshd privilege separation directory: %v", err)
	}

	cmd := exec.Command("/usr/sbin/sshd", "-D", "-f", cfg)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sshd: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	waitForPort(t, port)
	return port
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("sshd did not start listening on port %d", port)
}

func testEnv(t *testing.T) (*store.DB, *Service, *store.Host) {
	t.Helper()
	requireSSHD(t)

	db, err := store.Open(filepath.Join(t.TempDir(), "deployer.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	id, err := sshx.EnsureIdentity(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	port := startSSHD(t, id.AuthorizedKey())

	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	h, err := db.CreateHost(context.Background(), &store.Host{
		Name: "localhost", Address: "127.0.0.1", Port: port, Username: me.Username,
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, NewService(db, id), h
}

func TestProbeAgainstRealHost(t *testing.T) {
	db, svc, h := testEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	probe, err := svc.Probe(ctx, h)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	if probe.Facts.Hostname == "" || probe.Facts.Arch == "" || probe.Facts.Kernel == "" {
		t.Errorf("facts incomplete: %+v", probe.Facts)
	}
	if !strings.Contains(strings.ToLower(probe.Facts.OS), "linux") &&
		!strings.Contains(strings.ToLower(probe.Facts.OS), "ubuntu") &&
		!strings.Contains(strings.ToLower(probe.Facts.OS), "debian") {
		t.Errorf("os = %q, want something recognizable", probe.Facts.OS)
	}
	if probe.Sample.MemTotal <= 0 || probe.Sample.MemUsed <= 0 {
		t.Errorf("memory = %d/%d, want positive values", probe.Sample.MemUsed, probe.Sample.MemTotal)
	}
	if probe.Sample.MemUsed > probe.Sample.MemTotal {
		t.Errorf("memUsed %d exceeds memTotal %d", probe.Sample.MemUsed, probe.Sample.MemTotal)
	}
	if probe.Sample.CPUPct < 0 || probe.Sample.CPUPct > 100 {
		t.Errorf("cpuPct = %v, out of range", probe.Sample.CPUPct)
	}
	if probe.Sample.UptimeS <= 0 {
		t.Errorf("uptimeS = %d, want positive", probe.Sample.UptimeS)
	}
	if len(probe.Sample.Disks) == 0 {
		t.Error("no filesystems reported")
	}
	for _, d := range probe.Sample.Disks {
		if d.TotalBytes <= 0 || d.UsedBytes > d.TotalBytes {
			t.Errorf("implausible disk %+v", d)
		}
	}

	// The probe result must land in the database.
	stored, err := db.GetHost(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != store.StatusOnline {
		t.Errorf("status = %q, want online", stored.Status)
	}
	if stored.LastSeenAt == nil {
		t.Error("lastSeenAt not recorded")
	}
	if stored.HostKey == "" {
		t.Error("host key was not pinned on first connect")
	}
	latest, err := db.LatestSamples(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if latest[h.ID] == nil {
		t.Fatal("sample was not stored")
	}
}

func TestPinnedHostKeyMismatchIsRejected(t *testing.T) {
	db, svc, h := testEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := svc.Probe(ctx, h); err != nil {
		t.Fatalf("first Probe: %v", err)
	}
	stored, _ := db.GetHost(ctx, h.ID)
	if stored.HostKey == "" {
		t.Fatal("expected a pinned host key")
	}

	// Simulate the host presenting a different key (reinstall, or a MITM).
	other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ9Z8pCBmC3Cq5V0f3n5Y7v0Yl8kQxKq1nqZ0000000 impostor"
	if err := db.SetHostKey(ctx, h.ID, other); err != nil {
		t.Fatal(err)
	}
	stored, _ = db.GetHost(ctx, h.ID)

	_, err := svc.Probe(ctx, stored)
	if !errors.Is(err, sshx.ErrHostKeyChanged) {
		t.Fatalf("Probe error = %v, want ErrHostKeyChanged", err)
	}
	after, _ := db.GetHost(ctx, h.ID)
	if after.Status != store.StatusError {
		t.Errorf("status = %q, want error after a host key mismatch", after.Status)
	}
}

func TestUnreachableHostIsMarkedOffline(t *testing.T) {
	requireSSHD(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "deployer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id, err := sshx.EnsureIdentity(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, id)

	ctx := context.Background()
	h, err := db.CreateHost(ctx, &store.Host{
		Name: "ghost", Address: "127.0.0.1", Port: freePort(t), Username: "nobody",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Probe(ctx, h); err == nil {
		t.Fatal("expected a connection error")
	}
	stored, _ := db.GetHost(ctx, h.ID)
	if stored.Status != store.StatusOffline {
		t.Errorf("status = %q, want offline", stored.Status)
	}
	if stored.LastError == "" {
		t.Error("lastError not recorded")
	}
}

func TestTestReportsHints(t *testing.T) {
	_, svc, h := testEnv(t)
	res := svc.Test(context.Background(), h)
	if !res.OK {
		t.Fatalf("Test failed: %+v", res)
	}
	if res.Hostname == "" {
		t.Error("hostname missing from a successful test")
	}
	if !res.SudoOK && len(res.Hints) == 0 {
		t.Error("sudo unavailable but no hint offered")
	}
}
