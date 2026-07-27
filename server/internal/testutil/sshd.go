// Package testutil provides a throwaway sshd for tests that exercise the real
// SSH path: key auth, command execution and the /proc probe.
package testutil

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// RequireSSHD skips the test unless a local sshd can be started.
func RequireSSHD(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/usr/sbin/sshd"); err != nil {
		t.Skipf("sshd not available: %v", err)
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skipf("ssh-keygen not available: %v", err)
	}
	if os.Geteuid() != 0 {
		t.Skip("sshd test server needs root")
	}
}

// StartSSHD boots an sshd that accepts only the given authorized key, and
// returns the port it listens on. It is stopped when the test ends.
func StartSSHD(t *testing.T, authorizedKey string) int {
	t.Helper()
	RequireSSHD(t)

	dir := t.TempDir()
	// sshd rejects world-readable key material regardless of StrictModes.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	hostKey := filepath.Join(dir, "host_ed25519")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", hostKey).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}

	authFile := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(authFile, []byte(authorizedKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	port := FreePort(t)
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

// FreePort returns a port that is free right now.
func FreePort(t *testing.T) int {
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
