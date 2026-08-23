package hostops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The remote session is three generated scripts and a unit file, all of which
// run on somebody else's machine. So they are tested by running them: against a
// real filesystem, through a real shell, with the parts that would install
// packages or talk to systemd stubbed out. What is left is exactly the logic
// Deployer wrote, which is the part that can be wrong.

// stubBin builds a directory of fake commands. Each is a shell script that does
// whatever the test needs it to; putting it first on PATH is what keeps apt-get
// off the machine running the tests.
func stubBin(t *testing.T, stubs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range stubs {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// rootStubs are what every one of these scripts needs to believe it is running
// on a host: a root uid, an account with a home, and a systemd to tell.
func rootStubs(extra map[string]string) map[string]string {
	stubs := map[string]string{
		"id":        `[ "$1" = -u ] && { echo 0; exit 0; }; echo root`,
		"getent":    `printf 'pi:x:1000:1000::/home/pi:/bin/sh\n'`,
		"systemctl": `exit 0`,
	}
	for name, body := range extra {
		stubs[name] = body
	}
	return stubs
}

func setupScriptFor(root, user, geometry, port, page, reset string) string {
	script := fmt.Sprintf(remoteSetupScript,
		remoteConfDir, remoteLibDir,
		fmt.Sprintf(remoteSessionScript, strings.Join(remoteBrowsers, " ")),
		fmt.Sprintf(remoteInstallScript, remoteConfDir, strings.Join(remoteBrowsers, " ")),
		remoteDisplay, remoteVNCPort, RemoteUnit)
	return asUser(script, root, user, geometry, port, page, reset)
}

// waitForFile reads a file a backgrounded command is expected to write. Setup
// returns before the install it launched has run, which is the whole point of
// detaching it.
func waitForFile(t *testing.T, path string) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		if body, err := os.ReadFile(path); err == nil {
			return string(body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s was never written", path)
	return ""
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// Every generated script has to be a script before it can be a correct one, and
// a syntax error would only show up as a session that will not start.
func TestGeneratedScriptsParse(t *testing.T) {
	scripts := map[string]string{
		"session": fmt.Sprintf(remoteSessionScript, strings.Join(remoteBrowsers, " ")),
		"install": fmt.Sprintf(remoteInstallScript, remoteConfDir, strings.Join(remoteBrowsers, " ")),
		"setup": fmt.Sprintf(remoteSetupScript, remoteConfDir, remoteLibDir,
			"# session", "# install", remoteDisplay, remoteVNCPort, RemoteUnit),
		"status": fmt.Sprintf(remoteStatusScript, MaxRemoteLogBytes,
			strings.Join(remoteBrowsersAndPieces(), " "), RemoteUnit),
		"start":  fmt.Sprintf(remoteStartScript, remoteConfDir, RemoteUnit),
		"remove": fmt.Sprintf(remoteRemoveScript, remoteConfDir, remoteLibDir, RemoteUnit),
	}
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
				t.Fatal(err)
			}
			if out, code := runScript(t, "sh -n "+path, ""); code != 0 {
				t.Fatalf("sh -n %s exited %d: %s", name, code, out)
			}
		})
	}
}

// Setup writes what a session is made of and hands the slow half — apt — to the
// host to get on with. The launch is stubbed here so nothing is installed; what
// is checked is that the files are right and that the install was started.
func TestSetupScriptWritesTheSessionAndStartsTheInstall(t *testing.T) {
	root := t.TempDir()
	launched := filepath.Join(root, "launched")
	bin := stubBin(t, rootStubs(map[string]string{
		"nohup": `printf '%s\n' "$*" >> ` + launched,
	}))

	out, code := runScript(t, setupScriptFor(root, "pi", "1280x800", "6080", "https://example.com/login", ""), "", bin)
	if code != 0 {
		t.Fatalf("setup exited %d: %s", code, out)
	}
	if got := strings.TrimSpace(out); got != "started" {
		t.Errorf("setup said %q, want it to report that the install started", got)
	}

	unit := read(t, filepath.Join(root, "etc/systemd/system", RemoteUnit))
	// No [Install] section is the whole reason a session cannot be enabled and
	// does not come back after a reboot.
	if strings.Contains(unit, "[Install]") {
		t.Error("the unit must stay static, so it cannot be enabled to run at boot")
	}
	if !strings.Contains(unit, "User=pi") {
		t.Errorf("the session must run as the SSH user, got:\n%s", unit)
	}
	// The session script reads no configuration of its own: systemd hands it
	// everything, which is what keeps a file on the host from being run.
	wantExec := fmt.Sprintf("ExecStart=%s/remote-session.sh 1280x800 %d %d 6080 %s %s/home/pi/.config/deployer-remote %s/home/pi/Downloads",
		remoteLibDir, remoteDisplay, remoteVNCPort, root+remoteConfDir, root, root)
	if !strings.Contains(unit, wantExec) {
		t.Errorf("unit runs the wrong command:\n%s\nwant it to contain:\n%s", unit, wantExec)
	}

	config := read(t, filepath.Join(root, remoteConfDir, "config"))
	for _, want := range []string{"PORT=6080", "GEOMETRY=1280x800", "DOWNLOADS=" + root + "/home/pi/Downloads"} {
		if !strings.Contains(config, want) {
			t.Errorf("config is missing %q:\n%s", want, config)
		}
	}
	if got := strings.TrimSpace(read(t, filepath.Join(root, remoteConfDir, "homepage"))); got != "https://example.com/login" {
		t.Errorf("homepage is %q", got)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(root, remoteConfDir, "setup.state"))); got != "running" {
		t.Errorf("setup state is %q, want the install to be reported as under way", got)
	}

	// Detached, for the same reason a self-update is: a phone that locks its
	// screen must not be able to kill apt half way through a package. Detached
	// also means the launch outlives the call, so the test waits for it.
	if got := waitForFile(t, launched); !strings.Contains(got, "setsid sh "+root+remoteLibDir+"/remote-install.sh") {
		t.Errorf("the install was launched as %q, want it detached", got)
	}
	for _, script := range []string{"remote-session.sh", "remote-install.sh"} {
		info, err := os.Stat(filepath.Join(root, remoteLibDir, script))
		if err != nil {
			t.Fatalf("%s: %v", script, err)
		}
		if info.Mode().Perm()&0o100 == 0 && script == "remote-session.sh" {
			t.Errorf("%s is not executable (%s)", script, info.Mode())
		}
	}
}

// Setting up again must not sign anybody out. The scripts and the unit are
// rewritten; the password and the browser profile are what stay.
func TestSetupScriptIsIdempotent(t *testing.T) {
	root := t.TempDir()
	bin := stubBin(t, rootStubs(map[string]string{"nohup": `exit 0`}))

	if _, code := runScript(t, setupScriptFor(root, "pi", "", "6080", "", ""), "", bin); code != 0 {
		t.Fatalf("first setup exited %d", code)
	}
	kept := filepath.Join(root, remoteConfDir, "password")
	if err := os.WriteFile(kept, []byte("hunter22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code := runScript(t, setupScriptFor(root, "pi", "1024x768", "6081", "https://example.com/", ""), "", bin); code != 0 {
		t.Fatalf("second setup exited %d", code)
	}
	if got := strings.TrimSpace(read(t, kept)); got != "hunter22" {
		t.Errorf("the password changed to %q on a second setup", got)
	}
	if got := read(t, filepath.Join(root, remoteConfDir, "config")); !strings.Contains(got, "PORT=6081") {
		t.Errorf("the second setup did not take: %s", got)
	}
}

// Without root there is no apt, no /etc and no unit, so setup says so rather
// than failing half way through with something less useful.
func TestSetupScriptNeedsRoot(t *testing.T) {
	root := t.TempDir()
	bin := stubBin(t, map[string]string{"id": `echo 1000`})
	_, code := runScript(t, setupScriptFor(root, "pi", "", "6080", "", ""), "", bin)
	if code != 3 {
		t.Errorf("setup exited %d without root, want 3 so the API can say what is missing", code)
	}
}

func installScriptFor(root, user, reset string) string {
	return asUser(fmt.Sprintf(remoteInstallScript, remoteConfDir, strings.Join(remoteBrowsers, " ")),
		root, user, reset)
}

// The installer is the half that takes minutes. It reports where it got to
// through a state file, because by then nothing is listening.
func TestInstallScriptStoresAPasswordAndReportsSuccess(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, remoteConfDir)
	if err := os.MkdirAll(etc, 0o750); err != nil {
		t.Fatal(err)
	}
	bin := stubBin(t, map[string]string{
		"apt-get": `exit 0`,
		// -storepasswd writes the obfuscated file x11vnc authenticates against.
		"x11vnc": `[ "$1" = -storepasswd ] && printf 'stored\n' > "$3"; exit 0`,
		"chown":  `exit 0`,
	})

	out, code := runScript(t, installScriptFor(root, "pi", ""), "", bin)
	if code != 0 {
		t.Fatalf("install exited %d: %s", code, out)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(etc, "setup.state"))); got != "ok" {
		t.Errorf("state is %q, want ok", got)
	}

	// Eight characters because that is all the VNC protocol carries; a longer
	// one would be cut in half by the server and read as a wrong password.
	password := strings.TrimSpace(read(t, filepath.Join(etc, "password")))
	if len(password) != 8 {
		t.Errorf("password is %q (%d characters), want 8", password, len(password))
	}
	if strings.ContainsAny(password, "lo01") {
		t.Errorf("password %q uses characters that are misread off a screen", password)
	}
	if _, err := os.Stat(filepath.Join(etc, "vncpasswd")); err != nil {
		t.Errorf("x11vnc's password file was not written: %v", err)
	}

	// A second run leaves the password alone: the point of running setup again
	// is usually a setting, and a new password would log the phone out of a
	// session somebody is in the middle of.
	if _, code := runScript(t, installScriptFor(root, "pi", ""), "", bin); code != 0 {
		t.Fatalf("second install exited %d", code)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(etc, "password"))); got != password {
		t.Errorf("password changed to %q on a second run", got)
	}
	// Unless it is asked for, which is the way back from one that leaked.
	if _, code := runScript(t, installScriptFor(root, "pi", "1"), "", bin); code != 0 {
		t.Fatalf("reset install exited %d", code)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(etc, "password"))); got == password {
		t.Error("a reset should have made a new password")
	}
}

// A failed install has to leave the reason behind it: the state file carries the
// exit status and the log carries what apt said.
func TestInstallScriptRecordsAFailure(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, remoteConfDir)
	if err := os.MkdirAll(etc, 0o750); err != nil {
		t.Fatal(err)
	}
	bin := stubBin(t, map[string]string{
		"apt-get": `printf 'E: Unable to fetch some archives\n' >&2; exit 100`,
	})
	if _, code := runScript(t, installScriptFor(root, "pi", ""), "", bin); code != 10 {
		t.Errorf("install exited %d, want 10 for a package list that would not update", code)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(etc, "setup.state"))); got != "failed:10" {
		t.Errorf("state is %q, want the exit status recorded", got)
	}
}

func statusScript() string {
	return fmt.Sprintf(remoteStatusScript, MaxRemoteLogBytes,
		strings.Join(remoteBrowsersAndPieces(), " "), RemoteUnit)
}

// Status is one round trip that has to answer for a host in any state, starting
// with a host where none of this has ever been run.
func TestStatusScriptOnAHostWithNothingSetUp(t *testing.T) {
	root := t.TempDir()
	bin := stubBin(t, map[string]string{
		"getent":    `printf 'pi:x:1000:1000::/home/pi:/bin/sh\n'`,
		"systemctl": `printf 'LoadState=not-found\nActiveState=inactive\nSubState=dead\n'`,
	})
	out, code := runScript(t, asUser(statusScript(), root, "pi"), "", bin)
	if code != 0 {
		t.Fatalf("status exited %d: %s", code, out)
	}
	session := parseRemoteStatus(out, "pi")
	if session.Setup != "absent" {
		t.Errorf("setup is %q, want absent", session.Setup)
	}
	if session.Ready || session.Running {
		t.Errorf("nothing is installed, so nothing is ready or running: %+v", session)
	}
	if len(session.Missing) == 0 {
		t.Error("status should name what is missing")
	}
	if session.Downloads != root+"/home/pi/Downloads" {
		t.Errorf("downloads is %q", session.Downloads)
	}
}

// And a host where it is all there and running, which is the state the screen
// spends its time in.
func TestStatusScriptOnAReadyHost(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, remoteConfDir)
	downloads := filepath.Join(root, "home/pi/Downloads")
	for _, dir := range []string{etc, downloads} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(etc, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("setup.state", "ok\n")
	write("setup.log", "== updating the package list\n== ready\n")
	write("config", "PORT=6081\nGEOMETRY=1024x768\nDISPLAY_NUM=99\n")
	write("password", "qm4rt7xz\n")
	write("homepage", "https://example.com/login\n")
	write("vncpasswd", "stored\n")
	if err := os.WriteFile(filepath.Join(downloads, "statement.pdf"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := stubBin(t, map[string]string{
		"getent":      `printf 'pi:x:1000:1000::/home/pi:/bin/sh\n'`,
		"systemctl":   `printf 'LoadState=loaded\nActiveState=active\nSubState=running\n'`,
		"Xvfb":        `exit 0`,
		"x11vnc":      `exit 0`,
		"websockify":  `exit 0`,
		"chromium":    `exit 0`,
		"firefox-esr": `exit 0`,
	})
	out, code := runScript(t, asUser(statusScript(), root, "pi"), "", bin)
	if code != 0 {
		t.Fatalf("status exited %d: %s", code, out)
	}

	session := parseRemoteStatus(out, "pi")
	if session.Setup != "ok" || !session.Ready || !session.Running {
		t.Fatalf("want a ready, running session, got %+v", session)
	}
	if len(session.Missing) != 0 {
		t.Errorf("nothing should be missing, got %v", session.Missing)
	}
	// Chromium first: it is what a Pi already has, and the one whose download
	// directory can be set without clicking through a settings screen over VNC.
	if session.Browser != "chromium" {
		t.Errorf("browser is %q, want the first one on the list that is installed", session.Browser)
	}
	if session.Port != 6081 || session.Geometry != "1024x768" {
		t.Errorf("settings did not come back: port %d, geometry %q", session.Port, session.Geometry)
	}
	if session.Password != "qm4rt7xz" || session.Homepage != "https://example.com/login" {
		t.Errorf("password or homepage did not come back: %+v", session)
	}
	if !strings.Contains(session.SetupLog, "== ready") {
		t.Errorf("setup log is %q", session.SetupLog)
	}
	if len(session.Files) != 1 || session.Files[0].Name != "statement.pdf" || session.Files[0].Size != 10 {
		t.Errorf("downloads did not come back: %+v", session.Files)
	}
	if session.Files[0].AgeS < 0 || session.Files[0].AgeS > 60 {
		t.Errorf("a file just written is %ds old", session.Files[0].AgeS)
	}
}

// A failed install is the state worth getting right: the screen has to be able
// to say what happened and show the end of the log.
func TestParseStatusReadsAFailedSetup(t *testing.T) {
	out := "@@state\nfailed:11\n@@log\n" +
		"PT0gaW5zdGFsbGluZwpFOiBVbmFibGUgdG8gbG9jYXRlIHBhY2thZ2Ugbm92bmMK\n" +
		"@@have\nXvfb\n@@unit\nLoadState=loaded\nActiveState=inactive\nSubState=dead\n"
	session := parseRemoteStatus(out, "pi")
	if session.Setup != "failed" || session.SetupExit != 11 {
		t.Errorf("want a failure with its exit status, got %q/%d", session.Setup, session.SetupExit)
	}
	if !strings.Contains(session.SetupLog, "Unable to locate package novnc") {
		t.Errorf("the log did not survive: %q", session.SetupLog)
	}
	if session.Ready {
		t.Error("a host missing half the session is not ready")
	}
	if !contains(session.Missing, "x11vnc") || !contains(session.Missing, "a browser") {
		t.Errorf("missing is %v, want what is not installed named", session.Missing)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// Starting points the session at a page and starts it in one round trip, which
// is what makes "open this site" a tap rather than a URL typed over VNC.
func TestStartScriptWritesThePageThenStarts(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, remoteConfDir)
	if err := os.MkdirAll(etc, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "config"), []byte("PORT=6080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "homepage"), []byte("https://old.example/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(root, "started")
	bin := stubBin(t, map[string]string{"systemctl": `printf '%s\n' "$*" >> ` + started})

	script := fmt.Sprintf(remoteStartScript, remoteConfDir, RemoteUnit)
	if _, code := runScript(t, asUser(script, root, "https://bank.example/login"), "", bin); code != 0 {
		t.Fatalf("start exited %d", code)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(etc, "homepage"))); got != "https://bank.example/login" {
		t.Errorf("homepage is %q", got)
	}
	if got := read(t, started); !strings.Contains(got, "start -- "+RemoteUnit) {
		t.Errorf("systemctl was asked for %q", got)
	}

	// Starting without naming a page keeps the last one rather than blanking it.
	if _, code := runScript(t, asUser(script, root, ""), "", bin); code != 0 {
		t.Fatalf("start exited %d", code)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(etc, "homepage"))); got != "https://bank.example/login" {
		t.Errorf("homepage became %q when no page was named", got)
	}
}

// Starting a session that was never set up is a mistake worth naming, not a
// systemctl error about a unit nobody has heard of.
func TestStartScriptRefusesAHostWithNoSession(t *testing.T) {
	root := t.TempDir()
	bin := stubBin(t, map[string]string{"systemctl": `exit 0`})
	script := fmt.Sprintf(remoteStartScript, remoteConfDir, RemoteUnit)
	if _, code := runScript(t, asUser(script, root, ""), "", bin); code != 3 {
		t.Errorf("start exited %d on a host with no session, want 3", code)
	}
}

// Removing takes what Deployer wrote and nothing else — the packages stay, the
// downloads stay, and the profile with its logins goes only when asked.
func TestRemoveScriptLeavesTheDownloadsAndTakesTheProfileOnlyWhenAsked(t *testing.T) {
	build := func(t *testing.T) (root string, bin string) {
		root = t.TempDir()
		dirs := []string{
			filepath.Join(root, remoteConfDir),
			filepath.Join(root, remoteLibDir),
			filepath.Join(root, "etc/systemd/system"),
			filepath.Join(root, "home/pi/Downloads"),
			filepath.Join(root, "home/pi/.config/deployer-remote/Default"),
		}
		for _, dir := range dirs {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for _, file := range []string{
			filepath.Join(root, "etc/systemd/system", RemoteUnit),
			filepath.Join(root, remoteLibDir, "remote-session.sh"),
			filepath.Join(root, remoteConfDir, "password"),
			filepath.Join(root, "home/pi/Downloads/statement.pdf"),
			filepath.Join(root, "home/pi/.config/deployer-remote/Default/Cookies"),
		} {
			if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root, stubBin(t, map[string]string{
			"systemctl": `exit 0`,
			"getent":    `printf 'pi:x:1000:1000::/home/pi:/bin/sh\n'`,
		})
	}
	script := fmt.Sprintf(remoteRemoveScript, remoteConfDir, remoteLibDir, RemoteUnit)

	t.Run("keeps the profile", func(t *testing.T) {
		root, bin := build(t)
		if _, code := runScript(t, asUser(script, root, "pi", ""), "", bin); code != 0 {
			t.Fatalf("remove exited %d", code)
		}
		for _, gone := range []string{
			filepath.Join(root, "etc/systemd/system", RemoteUnit),
			filepath.Join(root, remoteConfDir),
			filepath.Join(root, remoteLibDir, "remote-session.sh"),
		} {
			if _, err := os.Stat(gone); !os.IsNotExist(err) {
				t.Errorf("%s is still there", gone)
			}
		}
		for _, kept := range []string{
			filepath.Join(root, "home/pi/Downloads/statement.pdf"),
			filepath.Join(root, "home/pi/.config/deployer-remote/Default/Cookies"),
		} {
			if _, err := os.Stat(kept); err != nil {
				t.Errorf("%s should have been left alone: %v", kept, err)
			}
		}
	})

	t.Run("purges the profile when asked", func(t *testing.T) {
		root, bin := build(t)
		if _, code := runScript(t, asUser(script, root, "pi", "1"), "", bin); code != 0 {
			t.Fatalf("remove exited %d", code)
		}
		if _, err := os.Stat(filepath.Join(root, "home/pi/.config/deployer-remote")); !os.IsNotExist(err) {
			t.Error("the profile should have gone with a purge")
		}
		// The files somebody went to the trouble of downloading are not part of
		// the session, and are never what a removal means.
		if _, err := os.Stat(filepath.Join(root, "home/pi/Downloads/statement.pdf")); err != nil {
			t.Errorf("a purge took the downloads with it: %v", err)
		}
	})
}

// The session script is what actually runs on the virtual screen. It is checked
// here rather than run — starting an X server in a unit test would be testing
// Xvfb — but the parts Deployer got to choose are worth asserting.
func TestSessionScriptChoosesSafeDefaults(t *testing.T) {
	script := fmt.Sprintf(remoteSessionScript, strings.Join(remoteBrowsers, " "))
	for _, want := range []string{
		// The VNC server never listens on the network: the gateway is the only
		// way in, and it is the port Deployer's link points at.
		"-localhost",
		// Without this Chromium waits for a keyring no headless host will answer.
		"--password-store=basic",
		// Downloads land in the host's own directory without a dialog, which is
		// the entire point of doing this on the host at all.
		"prompt_for_download",
		"-rfbauth",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the session script should contain %q", want)
		}
	}
	if strings.Contains(script, "-nopw") {
		t.Error("the session must never offer VNC without a password")
	}
	// It is handed its settings by systemd and reads only the page to open, so
	// nothing on the host is fed to a shell that would run what is in it.
	if strings.Contains(script, ". \"$conf") || strings.Contains(script, "source ") {
		t.Error("the session script must not source anything")
	}
}

func TestCleanGeometry(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", DefaultRemoteGeometry, false},
		{"1280x800", "1280x800", false},
		{" 1024X768 ", "1024x768", false},
		{"640x480", "640x480", false},
		{"320x240", "", true},
		{"4000x2000", "", true},
		{"1280 x 800", "", true},
		{"1280x800; rm -rf /", "", true},
	}
	for _, tc := range cases {
		got, err := cleanGeometry(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("cleanGeometry(%q) = %q, want a refusal", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("cleanGeometry(%q): %v", tc.in, err)
		} else if got != tc.want {
			t.Errorf("cleanGeometry(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The page reaches the browser as an argument, so anything that could be read
// as an option, a local file or a script is not a page somebody asked for.
func TestCleanRemoteURL(t *testing.T) {
	for _, ok := range []string{"", "https://example.com/login", "http://192.168.2.5:8080/a?b=c"} {
		if _, err := CleanRemoteURL(ok); err != nil {
			t.Errorf("CleanRemoteURL(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{
		"javascript:alert(1)",
		"file:///etc/shadow",
		"--headless",
		"about:blank",
		"https://example.com/ --incognito",
		"https://example.com/`id`",
		"example.com",
	} {
		if got, err := CleanRemoteURL(bad); err == nil {
			t.Errorf("CleanRemoteURL(%q) = %q, want a refusal", bad, got)
		}
	}
}

func TestCleanRemotePort(t *testing.T) {
	if port, err := cleanRemotePort(0); err != nil || port != DefaultRemotePort {
		t.Errorf("an unset port should mean the default, got %d/%v", port, err)
	}
	if _, err := cleanRemotePort(80); err == nil {
		t.Error("a privileged port should be refused")
	}
	if _, err := cleanRemotePort(remoteVNCPort); err == nil {
		t.Error("the session's own VNC port should be refused")
	}
}

// The link is what the Open button hands to the phone's browser, so it has to
// connect on its own and fit the screen it lands on.
func TestRemoteURL(t *testing.T) {
	session := &RemoteSession{Port: 6080, Password: "qm4rt7xz"}
	got := RemoteURL("nakedpi.local", session)
	for _, want := range []string{
		"http://nakedpi.local:6080/vnc.html?",
		"autoconnect=1",
		"password=qm4rt7xz",
		"resize=scale",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RemoteURL = %q, want it to contain %q", got, want)
		}
	}
	if got := RemoteURL("fd00::1", session); !strings.HasPrefix(got, "http://[fd00::1]:6080/") {
		t.Errorf("an IPv6 host needs brackets, got %q", got)
	}
	if got := RemoteURL("", session); got != "" {
		t.Errorf("no address means no link, got %q", got)
	}
}

// The session script's own behaviour, run for real: a browser that will not
// start has to say so somewhere a person can read it. This is the test the
// first version of this file did not have, and its absence cost an evening —
// the browser's output went to /dev/null, so a session that never came up
// looked exactly like a session with nothing wrong except a black screen.
func TestSessionScriptReportsABrowserThatWillNotStart(t *testing.T) {
	// The script waits for the X server's socket at the path X itself uses, so
	// the stub has to put one there. A machine that will not allow that is one
	// where this test cannot run.
	const display = "91"
	socket := "/tmp/.X11-unix/X" + display
	if err := os.MkdirAll("/tmp/.X11-unix", 0o1777); err != nil {
		t.Skipf("no /tmp/.X11-unix to fake an X server in: %v", err)
	}
	if f, err := os.Create(socket); err != nil {
		t.Skipf("cannot write %s: %v", socket, err)
	} else {
		f.Close()
	}
	t.Cleanup(func() { os.Remove(socket) })

	root := t.TempDir()
	conf := filepath.Join(root, "conf")
	if err := os.MkdirAll(conf, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf, "homepage"), []byte("https://example.com/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := stubBin(t, map[string]string{
		"Xvfb":       `sleep 30`,
		"x11vnc":     `sleep 30`,
		"websockify": `sleep 30`,
		// The failure a real Chromium reports when it cannot use its profile —
		// on stderr, which is the stream that used to be thrown away.
		"chromium": `printf 'The profile appears to be in use by another Chromium process\n' >&2; exit 1`,
	})

	script := filepath.Join(root, "session.sh")
	if err := os.WriteFile(script, []byte(fmt.Sprintf(remoteSessionScript, strings.Join(remoteBrowsers, " "))), 0o755); err != nil {
		t.Fatal(err)
	}
	// It runs until it is stopped, the way systemd runs it, so the test stops it.
	run := exec.Command("timeout", "6", "sh", script, "1280x800", display, "5999", "6080",
		conf, filepath.Join(root, "profile"), filepath.Join(root, "Downloads"))
	run.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, _ := run.CombinedOutput()
	log := string(out)

	// The browser's own words, which are the only thing that ever says why.
	if !strings.Contains(log, "The profile appears to be in use") {
		t.Errorf("the browser's output never reached the journal:\n%s", log)
	}
	// Deployer's reading of them, for the times when the browser says nothing.
	if !strings.Contains(log, "exited after") {
		t.Errorf("a browser dying on the spot should be reported as such:\n%s", log)
	}
	// And which browser it was, since a distribution that ships a snap wrapper
	// leaves a binary on the PATH that cannot run.
	if !strings.Contains(log, "chromium is "+filepath.Join(bin, "chromium")) {
		t.Errorf("the session should name the browser it resolved:\n%s", log)
	}

	// The lock a crashed browser leaves behind is cleared on the way in;
	// leaving it is what turns one crash into a session that never works again.
	if _, err := os.Stat(filepath.Join(root, "profile", "SingletonLock")); err == nil {
		t.Error("a stale singleton lock survived the start")
	}
	// The profile was seeded before the browser ran, which is what stops
	// Chromium asking where to save every file over VNC.
	prefs, err := os.ReadFile(filepath.Join(root, "profile", "Default", "Preferences"))
	if err != nil {
		t.Fatalf("the profile was not seeded: %v", err)
	}
	if !strings.Contains(string(prefs), filepath.Join(root, "Downloads")) {
		t.Errorf("downloads are not pointed at the host's own directory: %s", prefs)
	}
}
