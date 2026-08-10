package hostops

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/chinmay28/deployer/server/internal/sshx"
	"github.com/chinmay28/deployer/server/internal/store"
)

// A machine runs hundreds of systemd services and cares about a handful. The
// ones worth a screen on a phone are the ones someone put there by hand — the
// unit files under /etc/systemd/system and /usr/local/lib/systemd/system — not
// the hundreds the distribution ships in /usr/lib/systemd/system. That is what
// "user created" means here, and it is decided by where the unit file lives,
// which is the same rule systemd itself uses to decide who wins.
//
// systemctl show is the source of the state: one call describes every unit in
// key=value lines, which parse the same on every version worth supporting, and
// unlike `status` it never wraps, colours or truncates what it says.

const (
	// MaxUnits bounds one listing. A host with more hand-written services than
	// this has bigger problems than a phone screen.
	MaxUnits = 300
	// MaxLogBytes bounds one journal fetch. The tail is what matters, so an
	// over-long answer keeps its end rather than its beginning.
	MaxLogBytes = 256 << 10
	// Log line counts the UI may ask for.
	MinLogLines     = 20
	MaxLogLines     = 2000
	DefaultLogLines = 200
)

// unitPattern is what systemd allows in a unit name, narrowed to services. The
// name reaches a command line as a quoted argument either way; this is what
// keeps a typo from becoming a confusing error from the host.
var unitPattern = regexp.MustCompile(`^[A-Za-z0-9_:@][A-Za-z0-9_.:@\\-]{0,238}\.service$`)

// unitActions is every action Deployer will ask systemd for. Anything else —
// masking, editing state files, isolating targets — belongs on a terminal, not
// on a phone.
var unitActions = map[string]bool{
	"start":   true,
	"stop":    true,
	"restart": true,
	"reload":  true,
	"enable":  true,
	"disable": true,
}

// Unit is one service, as systemd describes it.
type Unit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Load is systemd's LoadState: "loaded", "not-found", "masked", "error".
	Load string `json:"load"`
	// Active is ActiveState: "active", "inactive", "failed", "activating",
	// "deactivating".
	Active string `json:"active"`
	// Sub is the finer state within Active: "running", "exited", "dead".
	Sub string `json:"sub"`
	// FileState is UnitFileState: "enabled", "disabled", "static", "masked",
	// or empty where systemd has no opinion (a template, say).
	FileState string `json:"fileState"`
	// Path is the unit file systemd actually read, which is what the editor
	// opens. Empty for a unit with no fragment, such as a masked one.
	Path string `json:"path"`
	// Template marks a foo@.service, which is a pattern for instances rather
	// than something that can be started on its own.
	Template bool `json:"template"`
	MainPID  int  `json:"mainPid"`
	// Memory is what the unit's cgroup is using now; 0 where systemd does not
	// account for it.
	Memory int64 `json:"memory"`
	// Restarts is how many times systemd has restarted it by itself, which is
	// the difference between "running" and "flapping".
	Restarts int `json:"restarts"`
	// SinceS is how long it has been in its current state, in seconds.
	SinceS int64 `json:"sinceS"`
	// Result is why it last stopped — "success", "exit-code", "signal",
	// "timeout" — which is the first thing to know about a failed unit.
	Result string `json:"result"`
}

// UnitList is every hand-installed service on a host.
type UnitList struct {
	Units []Unit `json:"units"`
	// AsUser is who the commands ran as — root wherever sudo is available.
	AsUser string `json:"asUser"`
	// Truncated means the host has more unit files than MaxUnits.
	Truncated bool `json:"truncated"`
}

// UnitLog is the tail of one service's journal.
type UnitLog struct {
	Name    string `json:"name"`
	Lines   int    `json:"lines"`
	Content string `json:"content"`
	// Truncated means the log was longer than MaxLogBytes and the beginning
	// was dropped — the end is the part worth keeping.
	Truncated bool   `json:"truncated"`
	AsUser    string `json:"asUser"`
}

// showProps is the property set every screen is built from. Id comes first
// because it is what separates one unit's block from the next.
const showProps = `-p Id -p Description -p LoadState -p ActiveState -p SubState ` +
	`-p UnitFileState -p FragmentPath -p MainPID -p MemoryCurrent -p NRestarts ` +
	`-p Result -p ActiveEnterTimestampMonotonic -p InactiveEnterTimestampMonotonic`

// unitDirs is where an administrator's own unit files live. /usr/lib and /lib
// are the distribution's, and everything in them is somebody else's business.
// They are arguments rather than script text so a test can point the same
// script at a directory it built.
var unitDirs = []string{"/etc/systemd/system", "/usr/local/lib/systemd/system"}

// unitListScript names every service file in the directories it is given, then
// describes them all in one systemctl call. Globs that match nothing expand to
// themselves, hence the -e test; a directory that does not exist is skipped
// rather than being an error.
//
// The uptime goes back too: systemd reports state changes as microseconds since
// boot, and the machine's own clock is the only thing that turns one into "for
// three days".
const unitListScript = `set -u
limit=$1
shift
command -v systemctl >/dev/null 2>&1 || { printf 'systemd is not installed on this host\n' >&2; exit 2; }
printf '@@user\n%s\n' "$(id -un 2>/dev/null || echo unknown)"
printf '@@uptime\n%s\n' "$(cut -d' ' -f1 /proc/uptime 2>/dev/null || echo 0)"
seen=' '
names=''
n=0
for d in "$@"; do
  [ -d "$d" ] || continue
  for f in "$d"/*.service; do
    [ -e "$f" ] || [ -L "$f" ] || continue
    b=${f##*/}
    case "$b" in *[!A-Za-z0-9_.:@-]*) continue;; esac
    case "$seen" in *" $b "*) continue;; esac
    seen="$seen$b "
    n=$((n + 1))
    if [ "$n" -gt "$limit" ]; then printf '@@truncated\nyes\n'; break 2; fi
    names="$names $b"
  done
done
printf '@@units\n'
[ -n "$names" ] || exit 0
set -f
systemctl show --no-pager ` + showProps + ` -- $names
`

// Units lists the services someone installed on this host by hand.
//
// $names goes to systemctl unquoted so the shell splits it back into one
// argument per unit — safe because the loop above drops any name with a
// character systemd would not accept in one, and set -f stops what is left
// from being read as a glob.
func (s *Service) Units(ctx context.Context, h *store.Host) (*UnitList, error) {
	args := append([]string{strconv.Itoa(MaxUnits)}, unitDirs...)
	res, err := s.run(ctx, h, elevate(unitListScript, args...), "")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, unitError(res, h, "could not list the services on "+h.Name)
	}
	return parseUnitList(res.Stdout), nil
}

// showScript describes one unit, which need not be one of the hand-installed
// ones — a link from somewhere else in the UI is allowed to land anywhere.
const showScript = `set -u
u=$1
command -v systemctl >/dev/null 2>&1 || { printf 'systemd is not installed on this host\n' >&2; exit 2; }
printf '@@user\n%s\n' "$(id -un 2>/dev/null || echo unknown)"
printf '@@uptime\n%s\n' "$(cut -d' ' -f1 /proc/uptime 2>/dev/null || echo 0)"
printf '@@units\n'
systemctl show --no-pager ` + showProps + ` -- "$u"
`

// Unit describes one service. A name systemd has never heard of is not an
// error: it answers with LoadState=not-found, which is a state the UI shows
// rather than a failure it reports.
func (s *Service) Unit(ctx context.Context, h *store.Host, name string) (*Unit, error) {
	clean, err := CleanUnit(name)
	if err != nil {
		return nil, err
	}
	res, err := s.run(ctx, h, elevate(showScript, clean), "")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, unitError(res, h, "could not read "+clean)
	}
	list := parseUnitList(res.Stdout)
	if len(list.Units) == 0 {
		return nil, fmt.Errorf("the host said nothing about %s", clean)
	}
	return &list.Units[0], nil
}

// actionScript runs one systemctl verb against one unit. systemctl says what
// went wrong on stderr and nothing at all when it worked, so the marker is what
// tells success from a silent failure.
const actionScript = `set -u
u=$1
a=$2
command -v systemctl >/dev/null 2>&1 || { printf 'systemd is not installed on this host\n' >&2; exit 2; }
systemctl --no-pager "$a" -- "$u" || exit 3
printf 'done\n'
`

// Act runs start, stop, restart, reload, enable or disable against a service.
// It returns once systemd has finished — a start that waits for the service to
// come up waits here too, which is the answer worth having.
func (s *Service) Act(ctx context.Context, h *store.Host, name, action string) error {
	clean, err := CleanUnit(name)
	if err != nil {
		return err
	}
	if !unitActions[action] {
		return invalid("%q is not something Deployer will ask systemd to do", action)
	}
	res, err := s.run(ctx, h, elevate(actionScript, clean, action), "")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return unitError(res, h, fmt.Sprintf("could not %s %s", action, clean))
	}
	return nil
}

// reloadScript makes systemd re-read the unit files on disk. Editing a unit
// without this changes the file and nothing else.
const reloadScript = `set -u
command -v systemctl >/dev/null 2>&1 || { printf 'systemd is not installed on this host\n' >&2; exit 2; }
systemctl daemon-reload || exit 3
printf 'done\n'
`

// Reload runs `systemctl daemon-reload`.
func (s *Service) Reload(ctx context.Context, h *store.Host) error {
	res, err := s.run(ctx, h, elevate(reloadScript), "")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return unitError(res, h, "could not reload the unit files on "+h.Name)
	}
	return nil
}

// logScript prints the tail of a unit's journal, base64-encoded so no line of a
// log can be mistaken for one of the markers around it.
//
// --no-hostname drops a column that is the same on every line and costs a phone
// a third of its width; it arrived in systemd 230, so a host that refuses it
// gets the same command without it rather than no logs at all.
const logScript = `set -u
u=$1
n=$2
cap=$3
command -v journalctl >/dev/null 2>&1 || { printf 'journalctl is not installed on this host\n' >&2; exit 2; }
tmp=$(mktemp /tmp/deployer-log.XXXXXX) || { printf 'cannot write a temporary file\n' >&2; exit 3; }
trap 'rm -f "$tmp"' EXIT
journalctl --no-pager --no-hostname -o short-iso -n "$n" -u "$u" >"$tmp" 2>/dev/null ||
  journalctl --no-pager -o short-iso -n "$n" -u "$u" >"$tmp" || exit 4
printf '@@user\n%s\n' "$(id -un 2>/dev/null || echo unknown)"
printf '@@size\n%s\n' "$(wc -c <"$tmp" | tr -d ' ')"
printf '@@log\n'
tail -c "$cap" "$tmp" | base64
`

// Log returns the last lines of a service's journal, newest last. The tail is
// what is wanted, so a log that overruns MaxLogBytes loses its beginning.
func (s *Service) Log(ctx context.Context, h *store.Host, name string, lines int) (*UnitLog, error) {
	clean, err := CleanUnit(name)
	if err != nil {
		return nil, err
	}
	lines = clampLines(lines)
	cmd := elevate(logScript, clean, strconv.Itoa(lines), strconv.Itoa(MaxLogBytes))
	res, err := s.run(ctx, h, cmd, "")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, unitError(res, h, "could not read the log for "+clean)
	}
	return parseUnitLog(res.Stdout, clean, lines)
}

// clampLines keeps a request for the journal within what a phone can hold.
func clampLines(lines int) int {
	switch {
	case lines <= 0:
		return DefaultLogLines
	case lines < MinLogLines:
		return MinLogLines
	case lines > MaxLogLines:
		return MaxLogLines
	}
	return lines
}

// parseUnitLog decodes the journal and says whether its beginning was dropped.
func parseUnitLog(out, name string, lines int) (*UnitLog, error) {
	found := sections(out)
	log := &UnitLog{Name: name, Lines: lines, AsUser: first(found["user"])}

	raw, err := base64.StdEncoding.DecodeString(strings.Join(found["log"], ""))
	if err != nil {
		return nil, fmt.Errorf("the host returned something that is not a log: %w", err)
	}
	body := string(raw)
	if size, err := strconv.ParseInt(first(found["size"]), 10, 64); err == nil && size > int64(len(raw)) {
		log.Truncated = true
		// Taking the last bytes cuts the oldest line in half. A half line at
		// the top reads as corruption, so it goes.
		if cut := strings.IndexByte(body, '\n'); cut >= 0 {
			body = body[cut+1:]
		}
	}
	log.Content = body
	return log, nil
}

// parseUnitList reads the output of unitListScript or showScript.
func parseUnitList(out string) *UnitList {
	found := sections(out)
	list := &UnitList{
		Units:     []Unit{},
		AsUser:    first(found["user"]),
		Truncated: len(found["truncated"]) > 0,
	}
	uptime := parseSeconds(first(found["uptime"]))
	for _, unit := range parseShowBlocks(found["units"], uptime) {
		list.Units = append(list.Units, unit)
	}
	// By name, always. A list that reordered itself as services started and
	// stopped would move the row out from under the thumb reaching for it; the
	// filters above it are what put failures in front of you.
	sort.SliceStable(list.Units, func(i, j int) bool {
		return strings.ToLower(list.Units[i].Name) < strings.ToLower(list.Units[j].Name)
	})
	return list
}

// parseShowBlocks splits systemctl's key=value output into one unit per Id.
// systemctl prints properties in the order they were asked for and Id is asked
// for first, so an Id line is where the next unit begins.
func parseShowBlocks(lines []string, uptime float64) []Unit {
	var units []Unit
	var props map[string]string
	flush := func() {
		if props == nil {
			return
		}
		if unit, ok := unitFromProps(props, uptime); ok {
			units = append(units, unit)
		}
		props = nil
	}
	for _, line := range lines {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if key == "Id" {
			flush()
		}
		if props == nil {
			props = map[string]string{}
		}
		props[key] = value
	}
	flush()
	return units
}

// unitFromProps turns one block of properties into a Unit, dropping anything
// that did not come with a name.
func unitFromProps(props map[string]string, uptime float64) (Unit, bool) {
	name := strings.TrimSpace(props["Id"])
	if name == "" {
		return Unit{}, false
	}
	unit := Unit{
		Name:        name,
		Description: props["Description"],
		Load:        props["LoadState"],
		Active:      props["ActiveState"],
		Sub:         props["SubState"],
		FileState:   props["UnitFileState"],
		Path:        props["FragmentPath"],
		Result:      props["Result"],
		Template:    strings.HasSuffix(name, "@.service"),
		MainPID:     atoi(props["MainPID"]),
		Restarts:    atoi(props["NRestarts"]),
		Memory:      memoryBytes(props["MemoryCurrent"]),
	}
	// A running unit has been running since it went active; a stopped or
	// failed one has been that way since it went inactive. Reading the wrong
	// one of the pair is how a service that died an hour ago comes to claim it
	// has been up for a week.
	stamp := props["InactiveEnterTimestampMonotonic"]
	if unit.Active == "active" || unit.Active == "activating" || unit.Active == "reloading" {
		stamp = props["ActiveEnterTimestampMonotonic"]
	}
	unit.SinceS = sinceSeconds(stamp, uptime)
	return unit, true
}

// sinceSeconds turns systemd's microseconds-since-boot into an age. A zero
// stamp means it has never been in that state, which is not an age at all.
func sinceSeconds(stamp string, uptime float64) int64 {
	micros, err := strconv.ParseInt(strings.TrimSpace(stamp), 10, 64)
	if err != nil || micros <= 0 || uptime <= 0 {
		return 0
	}
	age := int64(uptime) - micros/1e6
	if age < 0 {
		return 0
	}
	return age
}

// memoryBytes reads MemoryCurrent, which is "[not set]" where the unit has no
// cgroup accounting and an unsigned -1 — a number larger than any machine's
// memory — on the versions that say it that way instead.
func memoryBytes(value string) int64 {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || n > math.MaxInt64 {
		return 0
	}
	return int64(n)
}

func atoi(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}

// parseSeconds reads /proc/uptime's first field.
func parseSeconds(value string) float64 {
	secs, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return secs
}

// CleanUnit normalizes a unit name from the browser. Deployer manages services,
// so a name without the suffix gets one rather than being refused — ".service"
// is the part nobody types.
func CleanUnit(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalid("a service name is required")
	}
	if !strings.Contains(name, ".") {
		name += ".service"
	}
	if !unitPattern.MatchString(name) {
		return "", invalid("%q is not a systemd service name", name)
	}
	return name, nil
}

// unitError separates "systemd refused you" from "systemd refused". Starting a
// service needs root, and where sudo is not set up systemd asks for a password
// nobody is there to type — which is worth naming rather than passing on as
// "Interactive authentication required".
func unitError(res *sshx.Result, h *store.Host, fallback string) error {
	detail := strings.ToLower(res.Stderr + res.Stdout)
	switch {
	case strings.Contains(detail, "interactive authentication required"),
		strings.Contains(detail, "access denied"),
		strings.Contains(detail, "permission denied"):
		return errNeedsRoot(h.Username, h.Name)
	}
	return failure(res, fallback)
}
