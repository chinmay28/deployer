package hostops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The boot script is the half of this feature that runs on someone else's
// machine, so it is tested by running it: through a real shell, against real
// files, with the same parser the SSH path uses. journalctl, last and vcgencmd
// are stood in for by scripts that answer the way the real ones do — including
// the ways they refuse, which is what the fallbacks in the script exist for and
// therefore the only way to prove those fallbacks work.
//
// PATH is replaced rather than prepended for these, because half of what is
// being tested is what the script does when a command is *not* there, and a
// stub cannot express absence.

// bootTools are the commands the script needs from the machine it runs on. Each
// is symlinked into the sandbox PATH so the script has a shell to work in
// without also inheriting whatever systemd the test machine happens to have.
// sh is on the list because what goes over the wire is `sh -c SCRIPT`, so the
// shell has to be reachable from the sandboxed PATH as well as everything the
// script then calls.
var bootTools = []string{
	"sh", "id", "date", "cut", "uname", "tr", "grep", "sed", "tail", "wc", "base64", "mktemp", "cat",
}

// sandbox builds a PATH holding only the base tools and the stubs given, and
// returns it. A stub with an empty body is not written at all, which is how a
// test says "this host does not have that command".
func sandbox(t *testing.T, stubs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range bootTools {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s is not on this machine, so the boot script cannot be run here", tool)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range stubs {
		if body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runBootScript runs the script inside a sandboxed PATH and parses what comes
// back, so every assertion is made against the same struct the API returns.
func runBootScript(t *testing.T, path string, logFiles ...string) *BootReport {
	t.Helper()
	args := append([]string{"20", "100", "50", fmt.Sprint(MaxBootLogBytes), "10"}, logFiles...)
	run := exec.Command("/bin/sh", "-c", asUser(bootScript, args...))
	run.Env = []string{"PATH=" + path}
	var out, errOut strings.Builder
	run.Stdout = &out
	run.Stderr = &errOut
	if err := run.Run(); err != nil {
		t.Fatalf("boot script failed: %v: %s", err, strings.TrimSpace(errOut.String()))
	}
	report, err := parseBootReport(out.String())
	if err != nil {
		t.Fatalf("parseBootReport: %v\n---\n%s", err, out.String())
	}
	return report
}

// journalStub answers the four ways the script asks. `reject` is a string that,
// when present in the arguments, makes it fail — which is how the test forces
// the script down its fallback for a journalctl too old to know an option.
func journalStub(reject string) string {
	return `#!/bin/sh
reject='` + reject + `'
if [ -n "$reject" ]; then
  for a in "$@"; do [ "$a" = "$reject" ] && exit 1; done
fi
case " $* " in
  *" --list-boots "*)
    printf '%s\n' ' -2 4a8bcd11 Fri 2025-08-08 11:00:00 UTC—Sun 2025-08-10 22:03:11 UTC'
    printf '%s\n' ' -1 4a8bcd22 Sun 2025-08-10 22:03:11 UTC—Mon 2025-08-11 09:11:02 UTC'
    printf '%s\n' '  0 4a8bcd33 Mon 2025-08-11 09:12:33 UTC—Mon 2025-08-11 12:00:00 UTC'
    exit 0;;
  *" -p warning "*)
    printf '%s\n' '2025-08-11T09:10:58+0000 kernel: Kernel panic - not syncing: Attempted to kill init!'
    exit 0;;
  *" -b -1 "*)
    printf '%s\n' '2025-08-11T09:10:40+0000 countroster[1180]: served GET / in 3ms'
    printf '%s\n' '2025-08-11T09:10:58+0000 kernel: Kernel panic - not syncing: Attempted to kill init!'
    exit 0;;
esac
exit 1
`
}

// emptyJournalStub is a host whose journal is kept in memory: it knows the
// commands and has nothing from before the restart, which is what journalctl
// does by exiting non-zero with a complaint on stderr.
const emptyJournalStub = `#!/bin/sh
case " $* " in
  *" --list-boots "*) printf '%s\n' '  0 4a8bcd33 Mon 2025-08-11 09:12:33 UTC—Mon 2025-08-11 12:00:00 UTC'; exit 0;;
esac
printf 'Specifying boot ID or boot offset has no effect, no persistent journal was found.\n' >&2
exit 1
`

const lastStub = `#!/bin/sh
for a in "$@"; do
  case "$a" in --time-format=iso) iso=yes;; esac
done
if [ "${iso:-no}" = yes ]; then
  printf '%s\n' 'reboot   system boot  6.6.51+rpt-rpi-v8 2025-08-11T09:12:33+00:00   still running'
  printf '%s\n' 'reboot   system boot  6.6.51+rpt-rpi-v8 2025-08-08T11:00:00+00:00 - 2025-08-11T09:11:02+00:00 (70:11)'
  exit 0
fi
exit 0
`

// lastWithoutIso is a util-linux too old for --time-format, which is the reason
// the script asks three ways.
const lastWithoutIso = `#!/bin/sh
for a in "$@"; do
  case "$a" in --time-format=*) exit 1;; esac
done
printf '%s\n' 'reboot   system boot  6.6.51+rpt-rpi-v8 Mon Aug 11 09:12   still running'
printf '%s\n' 'shutdown system down  6.6.51+rpt-rpi-v8 Mon Aug 11 09:11 - 09:12  (00:01)'
exit 0
`

const vcgencmdStub = `#!/bin/sh
[ "$1" = get_throttled ] && { printf 'throttled=0x50005\n'; exit 0; }
exit 1
`

func TestBootScriptReadsTheJournal(t *testing.T) {
	path := sandbox(t, map[string]string{
		"journalctl": journalStub(""),
		"last":       lastStub,
		"vcgencmd":   vcgencmdStub,
	})
	r := runBootScript(t, path)

	if r.Source != SourceJournal {
		t.Errorf("source = %q, want %q", r.Source, SourceJournal)
	}
	if !r.Journal {
		t.Error("the host has journalctl and the report should say so")
	}
	if r.BootsKept != 3 {
		t.Errorf("bootsKept = %d, want the three boots --list-boots names", r.BootsKept)
	}
	if !strings.Contains(r.LogTail, "served GET /") {
		t.Errorf("logTail = %q, want the end of the previous boot", r.LogTail)
	}
	// The panic is in the warnings chunk, which only the second journalctl call
	// fetches — so finding it proves both calls were made and both were parsed.
	sign := mustSign(t, r, SignPanic)
	if sign.Detail != "Attempted to kill init!" {
		t.Errorf("detail = %q, want the panic's reason", sign.Detail)
	}
	if r.Throttle == nil || !r.Throttle.UnderVoltage {
		t.Errorf("throttle = %+v, want the firmware's flags read", r.Throttle)
	}
	if r.AsUser == "" {
		t.Error("asUser is empty, want the account the script ran as")
	}
	if r.UptimeS <= 0 || r.BootedAt.IsZero() {
		t.Errorf("uptime = %d, bootedAt = %v, want both read from /proc", r.UptimeS, r.BootedAt)
	}
	if len(r.Restarts) != 2 || !r.Restarts[0].Current || !r.CleanKnown {
		t.Errorf("restarts = %+v (cleanKnown %v), want two boots from `last -x`", r.Restarts, r.CleanKnown)
	}
}

// --no-hostname arrived in systemd 230. A host that refuses it must get the
// same command without it rather than no log at all.
func TestBootScriptSurvivesAnOldJournalctl(t *testing.T) {
	path := sandbox(t, map[string]string{
		"journalctl": journalStub("--no-hostname"),
		"last":       lastStub,
	})
	r := runBootScript(t, path)

	if r.Source != SourceJournal {
		t.Fatalf("source = %q, want the journal read without --no-hostname", r.Source)
	}
	if _, err := parseBootReport(""); err == nil {
		t.Error("an empty answer should not parse")
	}
	if sign := mustSign(t, r, SignPanic); sign.Detail == "" {
		t.Error("the fallback call should have produced the same evidence")
	}
}

// The default on Debian: journald keeps its log in memory, so there is nothing
// from before the restart — but rsyslog's file has the lines that come just
// before the last kernel banner, which is exactly the end of that boot.
func TestBootScriptFallsBackToASyslogFile(t *testing.T) {
	dir := t.TempDir()
	syslog := filepath.Join(dir, "syslog")
	body := strings.Join([]string{
		"Aug 11 08:00:01 pi systemd[1]: Started Daily apt download activities.",
		"Aug 11 09:10:40 pi countroster[1180]: served GET / in 3ms",
		"Aug 11 09:10:58 pi kernel: Kernel panic - not syncing: Attempted to kill init!",
		// Everything from the banner on belongs to the boot that is running,
		// and must not come back as evidence about the one that ended.
		"Aug 11 09:12:33 pi kernel: Linux version 6.6.51+rpt-rpi-v8 (dom@buildbot)",
		"Aug 11 09:12:34 pi kernel: Machine model: Raspberry Pi 4 Model B Rev 1.4",
	}, "\n") + "\n"
	if err := os.WriteFile(syslog, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	path := sandbox(t, map[string]string{
		"journalctl": emptyJournalStub,
		"last":       lastStub,
	})
	r := runBootScript(t, path, filepath.Join(dir, "missing"), syslog)

	if r.Source != SourceLogFile {
		t.Fatalf("source = %q, want %q", r.Source, SourceLogFile)
	}
	if r.LogFile != syslog {
		t.Errorf("logFile = %q, want %q", r.LogFile, syslog)
	}
	if !strings.Contains(r.LogTail, "Kernel panic") {
		t.Errorf("logTail = %q, want the lines before the kernel banner", r.LogTail)
	}
	if strings.Contains(r.LogTail, "Linux version") || strings.Contains(r.LogTail, "Machine model") {
		t.Errorf("logTail = %q, want nothing from the boot that is still running", r.LogTail)
	}
	// The journal is there and has nothing from before the restart, which is
	// the state the screen offers to fix. Whether /var/log/journal exists is
	// the test machine's business rather than the script's, so it is asserted
	// against fixtures instead of here.
	if !r.Journal {
		t.Error("the host has journalctl and the report should say so")
	}
}

// A host with neither a journal that remembers nor a syslog file has only wtmp,
// and the script has to come back whole rather than failing.
func TestBootScriptWithNothingToRead(t *testing.T) {
	path := sandbox(t, map[string]string{"last": lastStub})
	r := runBootScript(t, path, filepath.Join(t.TempDir(), "syslog"))

	if r.Source != SourceNone {
		t.Errorf("source = %q, want %q", r.Source, SourceNone)
	}
	if r.Journal {
		t.Error("there is no journalctl in this sandbox")
	}
	if r.LogTail != "" {
		t.Errorf("logTail = %q, want nothing", r.LogTail)
	}
	if len(r.Restarts) == 0 {
		t.Error("wtmp survives all of this and should still be reported")
	}
	if r.Throttle != nil {
		t.Errorf("throttle = %+v, want nothing on a host with no vcgencmd", r.Throttle)
	}
}

// util-linux without --time-format still shows the shutdown records, which are
// the half that matters; it just cannot date them.
func TestBootScriptWithAnOldLast(t *testing.T) {
	path := sandbox(t, map[string]string{
		"journalctl": emptyJournalStub,
		"last":       lastWithoutIso,
	})
	r := runBootScript(t, path)

	if !r.CleanKnown {
		t.Error("`last -x` shows shutdown records, so cleanliness is knowable")
	}
	if len(r.Restarts) != 1 {
		t.Fatalf("restarts = %+v, want the one boot it names", r.Restarts)
	}
	if r.Restarts[0].Timed {
		t.Error("an old `last` prints no ISO stamp, so there is no time to report")
	}
	if !r.Restarts[0].Clean {
		t.Error("a shutdown record sits below the boot, so it was asked for")
	}
}

// Nothing in a log may be mistaken for one of the markers around it, and
// nothing in it may be lost: the log crosses the wire base64-encoded for
// exactly that reason, and this is the proof.
func TestBootScriptCarriesLogsIntact(t *testing.T) {
	dir := t.TempDir()
	syslog := filepath.Join(dir, "syslog")
	awkward := []string{
		"Aug 11 09:00:00 pi app[1]: @@tail",
		"Aug 11 09:00:01 pi app[1]: @@end",
		"Aug 11 09:00:02 pi app[1]: café ☕ — naïve",
		"Aug 11 09:00:03 pi app[1]: a='$HOME'; b=\"`whoami`\"",
		"Aug 11 09:00:04 pi app[1]: * ? [a-z] | & ; ( ) < >",
		"Aug 11 09:00:05 pi app[1]: %s %d %%",
	}
	body := strings.Join(awkward, "\n") + "\nAug 11 09:12:33 pi kernel: Linux version 6.6.51\n"
	if err := os.WriteFile(syslog, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	path := sandbox(t, map[string]string{"last": lastStub})
	r := runBootScript(t, path, syslog)

	for _, line := range awkward {
		if !strings.Contains(r.LogTail, line) {
			t.Errorf("logTail lost %q\n---\n%s", line, r.LogTail)
		}
	}
	// A line of log that says "@@end" must not be read as the end marker, or
	// half the report would go missing whenever an app logged one.
	if r.Source != SourceLogFile {
		t.Errorf("source = %q, want the report to have survived its own markers", r.Source)
	}
}
