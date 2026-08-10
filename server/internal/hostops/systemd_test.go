package hostops

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/chinmay28/deployer/server/internal/sshx"
	"github.com/chinmay28/deployer/server/internal/store"
)

// systemd is not on every build machine, and where it is, a test has no
// business starting anything. What is worth proving is the half Deployer owns:
// that the scripts find the right unit files and no others, that they hand
// systemctl one argument per unit, and that whatever systemctl answers is read
// back the way it was meant. So systemctl and journalctl are stood in for, and
// the scripts run for real against a real shell and a real filesystem.

const fakeSystemctl = `#!/bin/sh
dir=$FAKE_SYSTEMD_DIR
mode=
units=
while [ $# -gt 0 ]; do
  case "$1" in
    --no-pager|--) ;;
    -p) shift;;
    show) mode=show;;
    cat) mode=cat;;
    daemon-reload|reset-failed) mode=$1;;
    start|stop|restart|reload|enable|disable) mode=$1;;
    *) units="$units $1";;
  esac
  shift
done
if [ "$mode" = cat ]; then
  for u in $units; do [ -f "$dir/$u.exists" ] || exit 1; done
  exit 0
fi
if [ "$mode" = show ]; then
  for u in $units; do
    if [ -f "$dir/$u.props" ]; then cat "$dir/$u.props"
    else printf 'Id=%s\nDescription=%s\nLoadState=not-found\nActiveState=inactive\nSubState=dead\n' "$u" "$u"; fi
    echo
  done
  exit 0
fi
printf '%s%s\n' "$mode" "$units" >> "$dir/actions.log"
if [ -f "$dir/refuse" ]; then cat "$dir/refuse" >&2; exit 1; fi
exit 0
`

// fakeJournalctl only knows --no-hostname when it is told to, so the fallback
// the script carries for systemd before v230 is exercised rather than assumed.
const fakeJournalctl = `#!/bin/sh
dir=$FAKE_JOURNAL_DIR
unit=
n=10
while [ $# -gt 0 ]; do
  case "$1" in
    --no-hostname) [ -n "${FAKE_JOURNAL_MODERN:-}" ] || { echo "unrecognized option '--no-hostname'" >&2; exit 1; };;
    -u) unit=$2; shift;;
    -n) n=$2; shift;;
    *) ;;
  esac
  shift
done
[ -f "$dir/$unit" ] || { echo '-- No entries --'; exit 0; }
tail -n "$n" "$dir/$unit"
`

// withFakeSystemctl puts the stand-in on PATH and returns the directory it
// reads unit properties from and writes actions to.
func withFakeSystemctl(t *testing.T) (dir string, path []string) {
	t.Helper()
	bin, dir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(fakeSystemctl), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SYSTEMD_DIR", dir)
	return dir, []string{bin}
}

func withFakeJournalctl(t *testing.T) (dir string, path []string) {
	t.Helper()
	bin, dir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "journalctl"), []byte(fakeJournalctl), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_JOURNAL_DIR", dir)
	return dir, []string{bin}
}

// props writes what systemctl show will say about one unit.
func props(t *testing.T, dir, unit string, lines ...string) {
	t.Helper()
	body := "Id=" + unit + "\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, unit+".props"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// unitFile drops a file into a fake /etc/systemd/system.
func unitFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// listUnits runs the real listing script over the directories given.
func listUnits(t *testing.T, limit int, path []string, dirs ...string) *UnitList {
	t.Helper()
	args := append([]string{strconv.Itoa(limit)}, dirs...)
	out, code := runScript(t, asUser(unitListScript, args...), "", path...)
	if code != 0 {
		t.Fatalf("listing units exited %d", code)
	}
	return parseUnitList(out)
}

func names(list *UnitList) []string {
	found := make([]string, 0, len(list.Units))
	for _, u := range list.Units {
		found = append(found, u.Name)
	}
	return found
}

// The whole point of the screen is the services someone put on the machine, so
// the listing takes unit files and nothing else: not the .wants symlink farm
// systemctl enable builds, not drop-in directories, not the timer beside the
// service, and not a distribution directory it was never pointed at.
func TestUnitsListsOnlyServiceFilesInTheDirectoriesGiven(t *testing.T) {
	state, path := withFakeSystemctl(t)
	etc, local, vendor := t.TempDir(), t.TempDir(), t.TempDir()

	unitFile(t, etc, "photos.service")
	unitFile(t, etc, "backup.timer")
	unitFile(t, etc, "notes.txt")
	unitFile(t, local, "cache.service")
	unitFile(t, vendor, "ssh.service")
	if err := os.MkdirAll(filepath.Join(etc, "multi-user.target.wants"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(etc, "photos.service.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	props(t, state, "photos.service", "ActiveState=active", "SubState=running")

	list := listUnits(t, MaxUnits, path, etc, local, filepath.Join(etc, "nowhere"))
	if got := strings.Join(names(list), " "); got != "cache.service photos.service" {
		t.Errorf("listed %q, want the two service files in alphabetical order", got)
	}
	if list.Truncated {
		t.Error("truncated = true on a listing that fitted")
	}
	// A directory that was not asked about is not read, whatever is in it.
	_ = vendor
}

// A unit that appears in both directories is one service, not two.
func TestUnitsDedupesAcrossDirectories(t *testing.T) {
	_, path := withFakeSystemctl(t)
	etc, local := t.TempDir(), t.TempDir()
	unitFile(t, etc, "photos.service")
	unitFile(t, local, "photos.service")

	list := listUnits(t, MaxUnits, path, etc, local)
	if len(list.Units) != 1 {
		t.Fatalf("listed %v, want one photos.service", names(list))
	}
}

func TestUnitsStopsAtTheLimit(t *testing.T) {
	_, path := withFakeSystemctl(t)
	etc := t.TempDir()
	for i := 0; i < 6; i++ {
		unitFile(t, etc, fmt.Sprintf("svc-%d.service", i))
	}
	list := listUnits(t, 3, path, etc)
	if len(list.Units) != 3 || !list.Truncated {
		t.Errorf("listed %d units (truncated %v), want 3 and a warning", len(list.Units), list.Truncated)
	}
}

// A host with nothing hand-installed on it is an empty list, not a failure:
// that is the normal state of a machine before anything is set up.
func TestUnitsWithNothingInstalled(t *testing.T) {
	_, path := withFakeSystemctl(t)
	list := listUnits(t, MaxUnits, path, t.TempDir())
	if len(list.Units) != 0 {
		t.Errorf("listed %v, want nothing", names(list))
	}
	if list.AsUser == "" {
		t.Error("the listing should still say who it ran as")
	}
}

// Every field the UI shows comes out of systemctl's own words, so this is the
// parse that matters.
func TestUnitsReadTheStateSystemdReports(t *testing.T) {
	state, path := withFakeSystemctl(t)
	etc := t.TempDir()
	unitFile(t, etc, "photos.service")
	unitFile(t, etc, "backup.service")

	props(t, state, "photos.service",
		"Description=Photo sync",
		"LoadState=loaded",
		"ActiveState=active",
		"SubState=running",
		"UnitFileState=enabled",
		"FragmentPath=/etc/systemd/system/photos.service",
		"MainPID=4213",
		"MemoryCurrent=52428800",
		"NRestarts=2",
		"Result=success",
		// An hour before the uptime the script reports, in microseconds.
		"ActiveEnterTimestampMonotonic=1000000",
		"InactiveEnterTimestampMonotonic=0",
	)
	props(t, state, "backup.service",
		"Description=Nightly backup",
		"LoadState=loaded",
		"ActiveState=failed",
		"SubState=failed",
		"UnitFileState=disabled",
		"FragmentPath=/etc/systemd/system/backup.service",
		"MainPID=0",
		"MemoryCurrent=[not set]",
		"Result=exit-code",
		// A failed unit's age comes from when it went inactive, not from an
		// ActiveEnter stamp left over from the last time it worked.
		"ActiveEnterTimestampMonotonic=1000000",
		"InactiveEnterTimestampMonotonic=2000000",
	)

	list := listUnits(t, MaxUnits, path, etc)
	byUnit := map[string]Unit{}
	for _, u := range list.Units {
		byUnit[u.Name] = u
	}

	photos := byUnit["photos.service"]
	if photos.Description != "Photo sync" || photos.Active != "active" || photos.Sub != "running" {
		t.Errorf("photos = %+v, want the running service systemd described", photos)
	}
	if photos.FileState != "enabled" || photos.MainPID != 4213 || photos.Restarts != 2 {
		t.Errorf("photos = %+v, want enabled, pid 4213, 2 restarts", photos)
	}
	if photos.Memory != 52428800 {
		t.Errorf("memory = %d, want 52428800", photos.Memory)
	}
	if photos.Path != "/etc/systemd/system/photos.service" {
		t.Errorf("path = %q, want the fragment systemd read", photos.Path)
	}

	backup := byUnit["backup.service"]
	if backup.Active != "failed" || backup.Result != "exit-code" {
		t.Errorf("backup = %+v, want a failed unit that exited non-zero", backup)
	}
	if backup.Memory != 0 {
		t.Errorf("memory = %d, want 0 where systemd does not account for it", backup.Memory)
	}
	// Both units carry a stamp; each must be aged from the right one, and the
	// failed unit's inactive stamp is the later of the two.
	if backup.SinceS >= photos.SinceS || photos.SinceS <= 0 {
		t.Errorf("ages are photos %ds, backup %ds — the failed unit went down more recently",
			photos.SinceS, backup.SinceS)
	}
}

// A template is a pattern, not something that can be started, and the UI has to
// know the difference before it offers a Start button.
func TestUnitsMarkTemplates(t *testing.T) {
	state, path := withFakeSystemctl(t)
	etc := t.TempDir()
	unitFile(t, etc, "tunnel@.service")
	props(t, state, "tunnel@.service", "LoadState=loaded", "ActiveState=inactive")

	list := listUnits(t, MaxUnits, path, etc)
	if len(list.Units) != 1 || !list.Units[0].Template {
		t.Errorf("units = %+v, want tunnel@.service marked as a template", list.Units)
	}
}

// Descriptions are free text. A line that looks like one of the markers the
// output is split on has to stay part of the description.
func TestUnitDescriptionsAreNotMistakenForMarkers(t *testing.T) {
	state, path := withFakeSystemctl(t)
	etc := t.TempDir()
	unitFile(t, etc, "odd.service")
	props(t, state, "odd.service", "Description=@@units is not a section here", "ActiveState=active")

	list := listUnits(t, MaxUnits, path, etc)
	if len(list.Units) != 1 || list.Units[0].Description != "@@units is not a section here" {
		t.Errorf("units = %+v, want the description kept whole", list.Units)
	}
}

func TestUnitActionsReachSystemctl(t *testing.T) {
	state, path := withFakeSystemctl(t)
	for _, action := range []string{"start", "stop", "restart", "reload", "enable", "disable"} {
		out, code := runScript(t, asUser(actionScript, "photos.service", action), "", path...)
		if code != 0 {
			t.Fatalf("%s exited %d", action, code)
		}
		if !strings.Contains(out, "done") {
			t.Errorf("%s said %q, want the marker that says it worked", action, out)
		}
	}
	log, err := os.ReadFile(filepath.Join(state, "actions.log"))
	if err != nil {
		t.Fatal(err)
	}
	want := "start photos.service\nstop photos.service\nrestart photos.service\n" +
		"reload photos.service\nenable photos.service\ndisable photos.service\n"
	if string(log) != want {
		t.Errorf("systemctl saw:\n%s\nwant:\n%s", log, want)
	}
}

// systemd refusing the job is the answer, not a crash: the exit status has to
// come back so the API can pass systemd's complaint on.
func TestUnitActionCarriesSystemdsRefusal(t *testing.T) {
	state, path := withFakeSystemctl(t)
	const complaint = "Job for photos.service failed. See 'systemctl status photos.service'.\n"
	if err := os.WriteFile(filepath.Join(state, "refuse"), []byte(complaint), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runScript(t, asUser(actionScript, "photos.service", "start"), "", path...)
	if code == 0 {
		t.Fatalf("a refused start exited 0 (%q)", out)
	}
	if strings.Contains(out, "done") {
		t.Error("a refused start should not print the marker that says it worked")
	}
}

func TestDaemonReloadRuns(t *testing.T) {
	state, path := withFakeSystemctl(t)
	if _, code := runScript(t, asUser(reloadScript), "", path...); code != 0 {
		t.Fatalf("daemon-reload exited %d", code)
	}
	log, _ := os.ReadFile(filepath.Join(state, "actions.log"))
	if strings.TrimSpace(string(log)) != "daemon-reload" {
		t.Errorf("systemctl saw %q, want daemon-reload", log)
	}
}

// readLog runs the real log script and parses what comes back.
func readLog(t *testing.T, unit string, lines, cap int, path []string) (*UnitLog, int) {
	t.Helper()
	cmd := asUser(logScript, unit, strconv.Itoa(lines), strconv.Itoa(cap))
	out, code := runScript(t, cmd, "", path...)
	if code != 0 {
		return nil, code
	}
	log, err := parseUnitLog(out, unit, lines)
	if err != nil {
		t.Fatalf("parseUnitLog: %v", err)
	}
	return log, 0
}

func TestUnitLogReadsTheTail(t *testing.T) {
	dir, path := withFakeJournalctl(t)
	var body strings.Builder
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&body, "2026-08-10T03:%02d:00+0000 photos[42]: line %d\n", i%60, i)
	}
	if err := os.WriteFile(filepath.Join(dir, "photos.service"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	log, code := readLog(t, "photos.service", 5, MaxLogBytes, path)
	if code != 0 {
		t.Fatalf("reading the log exited %d", code)
	}
	if log.Truncated {
		t.Error("truncated = true on a log that fitted")
	}
	if !strings.Contains(log.Content, "line 50") || strings.Contains(log.Content, "line 45") {
		t.Errorf("log = %q, want the last five lines", log.Content)
	}
}

// A log longer than Deployer will carry loses its beginning, not its end — and
// never leaves half a line at the top for the reader to puzzle over.
func TestUnitLogTruncatesFromTheTop(t *testing.T) {
	dir, path := withFakeJournalctl(t)
	var body strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&body, "%03d %s\n", i, strings.Repeat("x", 40))
	}
	if err := os.WriteFile(filepath.Join(dir, "photos.service"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	log, code := readLog(t, "photos.service", 40, 300, path)
	if code != 0 {
		t.Fatalf("reading the log exited %d", code)
	}
	if !log.Truncated {
		t.Fatal("truncated = false on a log that was cut")
	}
	if !strings.HasSuffix(strings.TrimSpace(log.Content), strings.Repeat("x", 40)) {
		t.Errorf("log = %q, want it to end with the newest line", log.Content)
	}
	for _, line := range strings.Split(strings.TrimSpace(log.Content), "\n") {
		if len(line) != 44 {
			t.Errorf("kept a partial line %q; the cut should land on a line boundary", line)
		}
	}
}

// --no-hostname is younger than the oldest systemd worth supporting, so a host
// that rejects it gets its logs anyway.
func TestUnitLogFallsBackWhenNoHostnameIsUnknown(t *testing.T) {
	dir, path := withFakeJournalctl(t)
	if err := os.WriteFile(filepath.Join(dir, "photos.service"), []byte("started\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log, code := readLog(t, "photos.service", 20, MaxLogBytes, path)
	if code != 0 {
		t.Fatalf("reading the log exited %d", code)
	}
	if strings.TrimSpace(log.Content) != "started" {
		t.Errorf("log = %q, want the fallback command's output", log.Content)
	}

	t.Setenv("FAKE_JOURNAL_MODERN", "1")
	if log, code = readLog(t, "photos.service", 20, MaxLogBytes, path); code != 0 {
		t.Fatalf("reading the log exited %d on a host that knows --no-hostname", code)
	}
	if strings.TrimSpace(log.Content) != "started" {
		t.Errorf("log = %q, want the same output either way", log.Content)
	}
}

// A service that has never logged anything is not an error.
func TestUnitLogWithNoEntries(t *testing.T) {
	_, path := withFakeJournalctl(t)
	log, code := readLog(t, "quiet.service", 20, MaxLogBytes, path)
	if code != 0 {
		t.Fatalf("reading an empty log exited %d", code)
	}
	if !strings.Contains(log.Content, "No entries") {
		t.Errorf("log = %q, want journalctl's own way of saying there is nothing", log.Content)
	}
}

func TestCleanUnit(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "photos.service", want: "photos.service"},
		{in: "  photos.service  ", want: "photos.service"},
		// ".service" is the part nobody types.
		{in: "photos", want: "photos.service"},
		{in: "tunnel@.service", want: "tunnel@.service"},
		{in: "tunnel@home.service", want: "tunnel@home.service"},
		{in: "my-app_2.service", want: "my-app_2.service"},
		{in: "", wantErr: true},
		{in: "photos.timer", wantErr: true},
		{in: "photos.socket", wantErr: true},
		{in: "photos.service; rm -rf /", wantErr: true},
		{in: "photos service.service", wantErr: true},
		{in: "-photos.service", wantErr: true},
		{in: "../etc/passwd.service", wantErr: true},
		{in: "$(touch pwned).service", wantErr: true},
		{in: strings.Repeat("u", 300) + ".service", wantErr: true},
	}
	for _, tc := range cases {
		got, err := CleanUnit(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("CleanUnit(%q) = %q, want an error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("CleanUnit(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CleanUnit(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestActOnlyRunsTheVerbsDeployerOffers(t *testing.T) {
	svc := &Service{}
	h := &store.Host{Name: "pi", Username: "chinmay"}
	for _, action := range []string{"mask", "isolate", "kill", "poweroff", "", "start; reboot"} {
		err := svc.Act(t.Context(), h, "photos.service", action)
		if err == nil {
			t.Errorf("Act(%q) was allowed", action)
			continue
		}
		if !strings.Contains(err.Error(), action) && action != "" {
			t.Errorf("Act(%q) said %q, which does not name what was refused", action, err)
		}
	}
}

// systemd asking for a password nobody is there to type is a missing-sudo
// problem, and saying so is more use than passing polkit's wording on.
func TestUnitErrorNamesMissingRoot(t *testing.T) {
	h := &store.Host{Name: "pi", Username: "chinmay"}
	polkit := &sshx.Result{ExitCode: 1, Stderr: "Failed to restart photos.service: Interactive authentication required."}
	err := unitError(polkit, h, "could not restart")
	if !strings.Contains(err.Error(), "sudo") || !strings.Contains(err.Error(), "chinmay") {
		t.Errorf("error = %q, want it to name the account that needs sudo", err)
	}

	refused := &sshx.Result{ExitCode: 1, Stderr: "Unit photos.service not found."}
	if got := unitError(refused, h, "could not restart").Error(); got != "Unit photos.service not found." {
		t.Errorf("error = %q, want systemd's own words", got)
	}
}

func TestSinceSeconds(t *testing.T) {
	const uptime = 10_000.0
	cases := []struct {
		stamp string
		want  int64
	}{
		{stamp: "1000000", want: 9999},     // one second after boot
		{stamp: "9000000000", want: 1000},  // 9000 seconds after boot
		{stamp: "0", want: 0},              // never been in that state
		{stamp: "", want: 0},               // the version does not report it
		{stamp: "not a number", want: 0},   //
		{stamp: "99000000000000", want: 0}, // clocks disagreeing is not a negative age
	}
	for _, tc := range cases {
		if got := sinceSeconds(tc.stamp, uptime); got != tc.want {
			t.Errorf("sinceSeconds(%q) = %d, want %d", tc.stamp, got, tc.want)
		}
	}
	if got := sinceSeconds("1000000", 0); got != 0 {
		t.Errorf("sinceSeconds with no uptime = %d, want 0", got)
	}
}

func TestMemoryBytes(t *testing.T) {
	cases := map[string]int64{
		"52428800":             52428800,
		"0":                    0,
		"[not set]":            0,
		"":                     0,
		"18446744073709551615": 0, // an unsigned -1: no accounting, not 16 exabytes
	}
	for in, want := range cases {
		if got := memoryBytes(in); got != want {
			t.Errorf("memoryBytes(%q) = %d, want %d", in, got, want)
		}
	}
}

// systemctl separates units with a blank line, but the Id line is what is
// relied on — a version that drops the blank line still parses.
func TestParseShowBlocksSplitOnId(t *testing.T) {
	lines := []string{
		"Id=a.service", "ActiveState=active",
		"Id=b.service", "ActiveState=failed",
		"", "Id=c.service", "ActiveState=inactive",
	}
	units := parseShowBlocks(lines, 0)
	if len(units) != 3 {
		t.Fatalf("parsed %d units, want 3: %+v", len(units), units)
	}
	if units[0].Active != "active" || units[1].Active != "failed" || units[2].Active != "inactive" {
		t.Errorf("units = %+v, want each state with its own unit", units)
	}
}

func TestClampLines(t *testing.T) {
	cases := map[int]int{
		0:     DefaultLogLines,
		-5:    DefaultLogLines,
		1:     MinLogLines,
		200:   200,
		99999: MaxLogLines,
	}
	for in, want := range cases {
		if got := clampLines(in); got != want {
			t.Errorf("clampLines(%d) = %d, want %d", in, got, want)
		}
	}
}

// createUnit runs the real install script the way the SSH path would.
func createUnit(t *testing.T, name, dir, content string, path []string) (string, int) {
	t.Helper()
	body := base64.StdEncoding.EncodeToString([]byte(ensureFinalNewline(content)))
	return runScript(t, asUser(writeUnitScript, name, dir), body, path...)
}

func TestCreateUnitWritesTheFileAndReloadsSystemd(t *testing.T) {
	state, path := withFakeSystemctl(t)
	dir := t.TempDir()
	props(t, state, "photos.service", "Description=Photo sync", "LoadState=loaded", "ActiveState=inactive")

	const content = "[Unit]\nDescription=Photo sync\n\n[Service]\nExecStart=/usr/local/bin/sync\n"
	out, code := createUnit(t, "photos.service", dir, content, path)
	if code != 0 {
		t.Fatalf("creating the unit exited %d", code)
	}

	written := filepath.Join(dir, "photos.service")
	body, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("the unit file was not written: %v", err)
	}
	if string(body) != content {
		t.Errorf("wrote %q, want %q", body, content)
	}
	// Readable by everyone, writable by root: what systemd expects of a unit.
	info, err := os.Stat(written)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode is %o, want 644 — mktemp's 600 has to be widened", perm)
	}

	// A unit file systemd has not been told about is a file, not a service.
	log, _ := os.ReadFile(filepath.Join(state, "actions.log"))
	if !strings.Contains(string(log), "daemon-reload") {
		t.Errorf("systemctl saw %q, want a daemon-reload after the write", log)
	}

	// And the answer describes what was just created, so the UI can show it
	// without asking again.
	list := parseUnitList(out)
	if len(list.Units) != 1 || list.Units[0].Name != "photos.service" || list.Units[0].Load != "loaded" {
		t.Errorf("units = %+v, want the service that was just written", list.Units)
	}
}

// Writing over an existing unit file would replace something someone else put
// there, with no way back.
func TestCreateUnitRefusesToWriteOverAFile(t *testing.T) {
	_, path := withFakeSystemctl(t)
	dir := t.TempDir()
	unitFile(t, dir, "photos.service")

	out, code := createUnit(t, "photos.service", dir, "[Unit]\nDescription=different\n", path)
	if code == 0 {
		t.Fatalf("overwriting an existing unit file succeeded (%q)", out)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "photos.service")); string(body) != "[Unit]\n" {
		t.Errorf("the existing file is now %q, want it untouched", body)
	}
}

// A unit in /etc/systemd/system named after one the distribution ships does not
// replace it, it shadows it. Doing that by accident from a phone — to sshd, of
// all things — is not a mistake to leave available.
func TestCreateUnitRefusesANameSystemdAlreadyKnows(t *testing.T) {
	state, path := withFakeSystemctl(t)
	dir := t.TempDir()
	// The distribution's sshd: systemd knows the name, though nothing of it is
	// in the directory Deployer writes to.
	if err := os.WriteFile(filepath.Join(state, "ssh.service.exists"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := createUnit(t, "ssh.service", dir, "[Unit]\nDescription=mine now\n", path)
	if code == 0 {
		t.Fatalf("shadowing the distribution's ssh.service was allowed (%q)", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "ssh.service")); err == nil {
		t.Error("the unit file was written anyway")
	}
}

// Deleting takes the enable symlinks, the file, and the drop-in overrides that
// are meaningless without it — and clears the failed state, so a service that
// died on the way out does not haunt `systemctl --failed` after it is gone.
func TestRemoveUnitTakesTheFileAndItsDropIns(t *testing.T) {
	state, path := withFakeSystemctl(t)
	dir := t.TempDir()
	unitFile(t, dir, "photos.service")
	unit := filepath.Join(dir, "photos.service")
	if err := os.MkdirAll(unit+".d", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unit+".d", "override.conf"), []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runScript(t, asUser(removeUnitScript, "photos.service", unit), "", path...)
	if code != 0 {
		t.Fatalf("deleting the unit exited %d", code)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("said %q, want the marker that says it worked", out)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Error("the unit file is still there")
	}
	if _, err := os.Stat(unit + ".d"); !os.IsNotExist(err) {
		t.Error("the drop-in directory outlived the unit it belonged to")
	}

	log, _ := os.ReadFile(filepath.Join(state, "actions.log"))
	for _, want := range []string{"disable photos.service", "daemon-reload", "reset-failed photos.service"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("systemctl never saw %q. It saw:\n%s", want, log)
		}
	}
}

func TestRemoveUnitOnAFileThatIsNotThere(t *testing.T) {
	_, path := withFakeSystemctl(t)
	missing := filepath.Join(t.TempDir(), "gone.service")
	if _, code := runScript(t, asUser(removeUnitScript, "gone.service", missing), "", path...); code == 0 {
		t.Error("deleting a unit file that does not exist reported success")
	}
}

// The two things deleting must never do: take a service that is still running,
// or take a file that belongs to the package manager.
func TestRemovableRefusesWhatMustNotBeDeleted(t *testing.T) {
	local := "/etc/systemd/system/photos.service"
	cases := []struct {
		name string
		unit Unit
		want error
	}{
		{"a stopped service", Unit{Name: "photos.service", Active: "inactive", Path: local}, nil},
		{"a failed service", Unit{Name: "photos.service", Active: "failed", Path: local}, nil},
		{"one installed in /usr/local", Unit{Name: "a.service", Active: "inactive",
			Path: "/usr/local/lib/systemd/system/a.service"}, nil},
		{"a running service", Unit{Name: "photos.service", Active: "active", Path: local}, ErrUnitRunning},
		{"one still starting", Unit{Name: "photos.service", Active: "activating", Path: local}, ErrUnitRunning},
		{"one still stopping", Unit{Name: "photos.service", Active: "deactivating", Path: local}, ErrUnitRunning},
		{"the distribution's own", Unit{Name: "ssh.service", Active: "inactive",
			Path: "/usr/lib/systemd/system/ssh.service"}, ErrInvalid},
		{"one on the older vendor path", Unit{Name: "ssh.service", Active: "inactive",
			Path: "/lib/systemd/system/ssh.service"}, ErrInvalid},
		{"one systemd has no file for", Unit{Name: "ghost.service", Active: "inactive"}, ErrInvalid},
		{"one below the unit directory", Unit{Name: "a.service", Active: "inactive",
			Path: "/etc/systemd/system/multi-user.target.wants/a.service"}, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := removable(&tc.unit)
			if tc.want == nil {
				if err != nil {
					t.Errorf("removable = %v, want it allowed", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("removable = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestOwnUnitFile(t *testing.T) {
	cases := map[string]bool{
		"/etc/systemd/system/photos.service":                true,
		"/usr/local/lib/systemd/system/photos.service":      true,
		"/usr/lib/systemd/system/ssh.service":               false,
		"/lib/systemd/system/ssh.service":                   false,
		"/run/systemd/generator/x.service":                  false,
		"/etc/systemd/system/x.target.wants/photos.service": false,
		"/etc/systemd/user/photos.service":                  false,
		// Cleaned before it is judged, so neither a tidy path nor one that
		// tries to walk its way in is taken at face value.
		"/etc/systemd/system/./photos.service":                            true,
		"/etc/systemd/system/x.target.wants/../photos.service":            true,
		"/etc/systemd/system/../../../usr/lib/systemd/system/ssh.service": false,
		"/usr/lib/systemd/system/../../etc/systemd/system/photos.service": false,
	}
	for in, want := range cases {
		if got := ownUnitFile(in); got != want {
			t.Errorf("ownUnitFile(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLoadError(t *testing.T) {
	cases := map[string]string{
		`org.freedesktop.DBus.Error.InvalidArgs "Invalid argument"`: "Invalid argument",
		`org.freedesktop.systemd1.NoSuchUnit "Unit not found."`:     "Unit not found.",
		`n/a "n/a"`: "n/a",
		"":          "",
		"  ":        "",
		"no quotes": "no quotes",
		`empty ""`:  `empty ""`,
	}
	for in, want := range cases {
		if got := loadError(in); got != want {
			t.Errorf("loadError(%q) = %q, want %q", in, got, want)
		}
	}
}
