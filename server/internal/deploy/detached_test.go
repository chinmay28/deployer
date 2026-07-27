package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chinmay28/deployer/server/internal/store"
)

func TestParseExitMarker(t *testing.T) {
	cases := []struct {
		name string
		log  string
		code int
		done bool
	}{
		{"still running", "installing...\nbuilding...\n", 0, false},
		{"succeeded", "done\n\n" + exitMarker + "0\n", 0, true},
		{"failed", "boom\n\n" + exitMarker + "7\n", 7, true},
		{"no trailing newline", "x\n" + exitMarker + "3", 3, true},
		{"garbage after marker", "x\n" + exitMarker + "not-a-number\n", 0, false},
		// Output that merely mentions the marker must not end the deployment
		// early; the last occurrence is the real one.
		{"marker in output", "echo " + exitMarker + "0\nstill going\n", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, done := parseExitMarker(tc.log)
			if done != tc.done || code != tc.code {
				t.Errorf("parseExitMarker = (%d, %v), want (%d, %v)", code, done, tc.code, tc.done)
			}
		})
	}
}

func TestStripExitMarkerKeepsOutput(t *testing.T) {
	log := "installing\ndone\n\n" + exitMarker + "0\n"
	got := stripExitMarker(log)
	if strings.Contains(got, exitMarker) {
		t.Errorf("marker survived: %q", got)
	}
	if !strings.Contains(got, "installing") || !strings.Contains(got, "done") {
		t.Errorf("output lost: %q", got)
	}
}

// The whole point of detaching: the command must outlive the SSH session that
// started it, because updating Deployer kills that session.
func TestDetachedCommandOutlivesItsSSHSession(t *testing.T) {
	e := newEnv(t)
	logPath := filepath.Join(t.TempDir(), "detached.log")
	marker := filepath.Join(t.TempDir(), "still-running")

	client, err := e.svc.Connect(context.Background(), e.host)
	if err != nil {
		t.Fatal(err)
	}
	script := startDetachedScript(
		"sleep 2; touch "+ShellQuote(marker)+"; echo finished", logPath)
	if _, err := client.Run(context.Background(), script); err != nil {
		t.Fatalf("start detached: %v", err)
	}
	// Drop the connection the way a restart would.
	client.Close()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the detached command died with its SSH session")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read detached log: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "finished") {
		t.Errorf("log missing output:\n%s", log)
	}
	if code, done := parseExitMarker(log); !done || code != 0 {
		t.Errorf("exit marker = (%d, %v), want (0, true):\n%s", code, done, log)
	}
}

// selfHostEnv marks the test host as the machine Deployer runs on, which is
// what turns a deployment of a self-update app into a detached one.
func selfHostEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	if err := e.db.SetHostSelf(context.Background(), e.host.ID, true, "test-machine-id"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := e.db.GetHost(context.Background(), e.host.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.host = reloaded
	return e
}

func TestSelfUpdateRunsDetached(t *testing.T) {
	e := selfHostEnv(t)
	app := e.app(t, &store.App{
		Name:           "Deployer",
		InstallCommand: "echo updating to {{ref}}; sleep 1; echo restarted",
		Params:         []store.Param{{Name: "ref", Label: "Version", Default: "main"}},
		SelfUpdate:     true,
	})

	started, err := e.runner.Start(context.Background(), app.ID, e.host.ID, map[string]string{"ref": "v2"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.DetachedLog == "" {
		t.Fatal("a self-update on the home host must run detached")
	}
	t.Cleanup(func() { os.Remove(started.DetachedLog) })

	dep := e.wait(t, started.ID)
	if dep.Status != store.DeploySucceeded {
		t.Fatalf("status = %q, error %q, log:\n%s", dep.Status, dep.Error, dep.Log)
	}
	if !strings.Contains(dep.Log, "updating to v2") || !strings.Contains(dep.Log, "restarted") {
		t.Errorf("log missing the command output:\n%s", dep.Log)
	}
	// The exit marker is bookkeeping and must never reach the user.
	if strings.Contains(dep.Log, exitMarker) {
		t.Errorf("exit marker leaked into the log:\n%s", dep.Log)
	}
	if !strings.Contains(dep.Log, "survives Deployer restarting itself") {
		t.Errorf("log should explain why this one is different:\n%s", dep.Log)
	}
	if _, err := e.db.FindInstallation(context.Background(), app.ID, e.host.ID); err != nil {
		t.Errorf("a successful self-update should record an installation: %v", err)
	}
}

func TestSelfUpdateFailureIsRecorded(t *testing.T) {
	e := selfHostEnv(t)
	app := e.app(t, &store.App{
		Name:           "Deployer",
		InstallCommand: "echo starting; echo bad >&2; exit 9",
		SelfUpdate:     true,
	})

	started, err := e.runner.Start(context.Background(), app.ID, e.host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(started.DetachedLog) })

	dep := e.wait(t, started.ID)
	if dep.Status != store.DeployFailed {
		t.Fatalf("status = %q, want failed", dep.Status)
	}
	if dep.ExitCode == nil || *dep.ExitCode != 9 {
		t.Errorf("exitCode = %v, want 9", dep.ExitCode)
	}
	// Detached output is captured through the file, stderr included.
	if !strings.Contains(dep.Log, "bad") {
		t.Errorf("stderr missing from log:\n%s", dep.Log)
	}
}

// A self-update on a host that is not this machine has no reason to detach.
func TestSelfUpdateOnAnotherHostRunsNormally(t *testing.T) {
	e := newEnv(t) // host is not marked as self
	app := e.app(t, &store.App{
		Name:           "Deployer",
		InstallCommand: "echo installing deployer elsewhere",
		SelfUpdate:     true,
	})
	started, err := e.runner.Start(context.Background(), app.ID, e.host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if started.DetachedLog != "" {
		t.Error("only the home host needs the detached path")
	}
	if dep := e.wait(t, started.ID); dep.Status != store.DeploySucceeded {
		t.Fatalf("status = %q: %s", dep.Status, dep.Error)
	}
}

// The restart case: a previous Deployer process started a detached update and
// died. A fresh Runner must pick the log back up and finish the record.
func TestResumeDetachedAfterRestart(t *testing.T) {
	e := selfHostEnv(t)
	app := e.app(t, &store.App{
		Name:           "Deployer",
		InstallCommand: "irrelevant, the command already ran",
		SelfUpdate:     true,
	})

	ctx := context.Background()
	logPath := filepath.Join(t.TempDir(), "resume.log")
	dep, err := e.db.CreateDeployment(ctx, &store.Deployment{
		AppID: app.ID, HostID: e.host.ID, Command: "echo hi",
		Params: map[string]string{"ref": "v3"}, DetachedLog: logPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The command kept running on the host while Deployer was down, and has
	// since finished.
	content := "==> installing\nbuilding the web app\nrestarting deployer.service\n\n" + exitMarker + "0\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// A brand new Runner, exactly as a restarted Deployer would have.
	fresh := NewRunner(e.db, e.svc, e.checker, discardLogger())
	fresh.ResumeDetached(ctx)

	got := e.wait(t, dep.ID)
	if got.Status != store.DeploySucceeded {
		t.Fatalf("status = %q, error %q, log:\n%s", got.Status, got.Error, got.Log)
	}
	if !strings.Contains(got.Log, "Reconnected after restart") {
		t.Errorf("log should say it was picked back up:\n%s", got.Log)
	}
	if !strings.Contains(got.Log, "restarting deployer.service") {
		t.Errorf("log written while Deployer was down was lost:\n%s", got.Log)
	}
	if strings.Contains(got.Log, exitMarker) {
		t.Errorf("exit marker leaked into the log:\n%s", got.Log)
	}
	in, err := e.db.FindInstallation(ctx, app.ID, e.host.ID)
	if err != nil {
		t.Fatalf("resume should record the installation: %v", err)
	}
	if in.Params["ref"] != "v3" {
		t.Errorf("saved params = %v, want the ones the deployment was started with", in.Params)
	}
}

func TestResumeDetachedReportsFailure(t *testing.T) {
	e := selfHostEnv(t)
	app := e.app(t, &store.App{Name: "Deployer", InstallCommand: "x", SelfUpdate: true})

	ctx := context.Background()
	logPath := filepath.Join(t.TempDir(), "resume-failed.log")
	dep, err := e.db.CreateDeployment(ctx, &store.Deployment{
		AppID: app.ID, HostID: e.host.ID, Command: "x", DetachedLog: logPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("rolling back\n\n"+exitMarker+"1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fresh := NewRunner(e.db, e.svc, e.checker, discardLogger())
	fresh.ResumeDetached(ctx)

	got := e.wait(t, dep.ID)
	if got.Status != store.DeployFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 1 {
		t.Errorf("exitCode = %v, want 1", got.ExitCode)
	}
	if _, err := e.db.FindInstallation(ctx, app.ID, e.host.ID); err == nil {
		t.Error("a failed update must not count as installed")
	}
}

// A restart must not mark a detached deployment as interrupted: it is still
// running on the host and will be resumed.
func TestInterruptLeavesDetachedDeploymentsAlone(t *testing.T) {
	e := selfHostEnv(t)
	app := e.app(t, &store.App{Name: "Deployer", InstallCommand: "x", SelfUpdate: true})
	other := e.app(t, &store.App{Name: "ordinary", InstallCommand: "x"})

	ctx := context.Background()
	detached, err := e.db.CreateDeployment(ctx, &store.Deployment{
		AppID: app.ID, HostID: e.host.ID, Command: "x", DetachedLog: "/tmp/whatever.log",
	})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := e.db.CreateDeployment(ctx, &store.Deployment{
		AppID: other.ID, HostID: e.host.ID, Command: "x",
	})
	if err != nil {
		t.Fatal(err)
	}

	n, err := e.db.InterruptRunningDeployments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("interrupted %d deployments, want only the ordinary one", n)
	}

	stillRunning, _ := e.db.GetDeployment(ctx, detached.ID)
	if stillRunning.Status != store.DeployRunning {
		t.Errorf("detached deployment = %q, want it left running for the resumer", stillRunning.Status)
	}
	interrupted, _ := e.db.GetDeployment(ctx, ordinary.ID)
	if interrupted.Status != store.DeployInterrupted {
		t.Errorf("ordinary deployment = %q, want interrupted", interrupted.Status)
	}

	resumable, err := e.db.ResumableDeployments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resumable) != 1 || resumable[0].ID != detached.ID {
		t.Errorf("resumable = %+v, want just the detached one", resumable)
	}
}

// `systemctl restart deployer` reaches Deployer as a SIGTERM, which triggers a
// graceful shutdown. That must not cancel the very update doing the restarting.
func TestShutdownLeavesDetachedDeploymentsRunning(t *testing.T) {
	e := selfHostEnv(t)
	app := e.app(t, &store.App{
		Name:           "Deployer",
		InstallCommand: "sleep 30",
		SelfUpdate:     true,
	})
	started, err := e.runner.Start(context.Background(), app.ID, e.host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(started.DetachedLog) })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	e.runner.Shutdown(ctx)

	dep, err := e.db.GetDeployment(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dep.Status != store.DeployRunning {
		t.Fatalf("status = %q, want it left running so the next process resumes it", dep.Status)
	}

	// And it is still on the list to resume.
	resumable, err := e.db.ResumableDeployments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(resumable) != 1 || resumable[0].ID != started.ID {
		t.Errorf("resumable = %+v, want the detached deployment", resumable)
	}
}

// An explicit cancel still stops watching a detached deployment.
func TestCancelStillWorksForDetachedDeployments(t *testing.T) {
	e := selfHostEnv(t)
	app := e.app(t, &store.App{Name: "Deployer", InstallCommand: "sleep 30", SelfUpdate: true})
	started, err := e.runner.Start(context.Background(), app.ID, e.host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(started.DetachedLog) })

	// Wait until it is genuinely running on the host, so this exercises
	// cancelling the follower rather than the launch.
	waitForFile(t, started.DetachedLog)
	if err := e.runner.Cancel(started.ID); err != nil {
		t.Fatal(err)
	}
	dep := e.wait(t, started.ID)
	if dep.Status != store.DeployCanceled {
		t.Errorf("status = %q, want canceled", dep.Status)
	}
	// Canceling only stops watching; the command is still out there.
	if !strings.Contains(dep.Log, "keeps running on the host") {
		t.Errorf("log should be honest about what cancel does:\n%s", dep.Log)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
}

// After a shutdown hands a detached deployment over, the old follower must not
// also record an outcome — otherwise two processes race to finalize the same
// row and the log you keep is whichever finished last.
func TestShutdownHandsOverWithoutRecording(t *testing.T) {
	e := selfHostEnv(t)
	app := e.app(t, &store.App{
		Name:           "Deployer",
		InstallCommand: "sleep 1; echo done",
		SelfUpdate:     true,
	})
	started, err := e.runner.Start(context.Background(), app.ID, e.host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(started.DetachedLog) })
	waitForFile(t, started.DetachedLog)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	e.runner.Shutdown(ctx)

	// Give the abandoned follower every chance to write something it shouldn't:
	// the command itself finishes during this window.
	time.Sleep(4 * time.Second)

	dep, err := e.db.GetDeployment(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dep.Status != store.DeployRunning {
		t.Fatalf("status = %q, want it still running for the next process", dep.Status)
	}
	if dep.FinishedAt != nil {
		t.Error("the abandoned follower recorded a finish time")
	}

	// The next process picks it up and finishes it exactly once.
	fresh := NewRunner(e.db, e.svc, e.checker, discardLogger())
	fresh.ResumeDetached(context.Background())
	resumed := e.wait(t, started.ID)
	if resumed.Status != store.DeploySucceeded {
		t.Fatalf("status = %q, error %q", resumed.Status, resumed.Error)
	}
	if !strings.Contains(resumed.Log, "Reconnected after restart") {
		t.Errorf("the resumed process should own the log:\n%s", resumed.Log)
	}
	if !strings.Contains(resumed.Log, "done") {
		t.Errorf("output written while Deployer was down was lost:\n%s", resumed.Log)
	}
}
