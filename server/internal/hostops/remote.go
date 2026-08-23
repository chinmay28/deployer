package hostops

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/chinmay28/deployer/server/internal/store"
)

// A remote session is a browser running on the host that you drive from your
// phone: a virtual screen with nothing on it but a browser, offered over VNC
// and reached through noVNC in an ordinary browser tab.
//
// It exists because some things can only be done by a person in front of a
// browser — signing in, clicking through a consent page, taking a file from
// behind a login — and the machine that should end up holding the file is the
// host, not the phone. A browser on the host downloads into the host's own
// Downloads directory, which is the whole point: no transfer, no second copy,
// no phone in the middle.
//
// Three decisions shape the rest of this file.
//
// **The session gets its own screen rather than the host's.** x11vnc attaching
// to :0 is the familiar recipe and it is the wrong one here: it needs a desktop
// to already be running, it cannot attach to a Wayland session at all — which
// is what Raspberry Pi OS has run by default since Bookworm — and what it shows
// is whatever the machine is showing, to anybody who walks past it. A private
// Xvfb screen works the same on a headless Pi as on one with a monitor, and
// nothing on the real display changes while you use it.
//
// **It is off unless you turn it on.** The unit has no [Install] section, so
// systemd calls it static: it cannot be enabled, and it does not come back
// after a reboot. A logged-in browser is a bearer of every credential you typed
// into it, and one that runs all week is a worse thing to own than one that
// runs for the two minutes it takes to fetch a file.
//
// **The profile survives.** The browser keeps its profile in the SSH user's
// home, so the site you signed into last week is still signed in this week.
// That is what makes the second download a tap rather than another round with
// an authenticator app — and it is also why removing the session offers to take
// the profile with it.
//
// Nothing here is sourced. Values Deployer writes to the host are read back by
// parsing, and the session script is handed its settings as arguments by
// systemd, so no file on the host is ever fed to a shell that would run what is
// in it.

const (
	// RemoteUnit is the session's systemd unit. It is an ordinary unit file in
	// the administrator's own directory, so the Services screen lists it, shows
	// its journal and can stop it, like anything else installed by hand.
	RemoteUnit = "deployer-remote.service"

	// remoteConfDir holds everything Deployer writes that is not a unit file:
	// the VNC password, the settings, the page to open, and the setup log.
	remoteConfDir = "/etc/deployer-remote"
	// remoteLibDir holds the two generated scripts. /usr/local is where things
	// that are not the package manager's belong.
	remoteLibDir = "/usr/local/lib/deployer"

	// DefaultRemoteGeometry is the virtual screen's size. A desktop-width
	// screen is what makes sites lay out the way their author meant, and noVNC
	// scales it down to whatever the phone actually has.
	DefaultRemoteGeometry = "1280x800"
	// DefaultRemotePort is where noVNC answers. 6080 is websockify's own
	// convention and is unlikely to collide with anything on a home server.
	DefaultRemotePort = 6080
	// remoteVNCPort is where x11vnc listens, bound to localhost so the only
	// door into the session is the one noVNC opens.
	remoteVNCPort = 5999
	// remoteDisplay is the X display the session runs on. :99 is high enough to
	// stay out of the way of a real desktop on :0.
	remoteDisplay = 99
)

// RemoteSession is everything the screen needs to know in one round trip.
type RemoteSession struct {
	// Unit is the systemd unit, so start and stop go through the service API
	// that already knows how to wait for systemd.
	Unit string `json:"unit"`
	// Setup is where the install got to: "absent" (never run), "running",
	// "ok", or "failed".
	Setup string `json:"setup"`
	// SetupExit is the exit status behind a failed setup, for the log to be
	// read against.
	SetupExit int `json:"setupExit,omitempty"`
	// SetupLog is the tail of the install log — the answer to "what is it
	// doing?" while apt works, and to "why not?" afterwards.
	SetupLog string `json:"setupLog,omitempty"`
	// Ready reports whether a session could be started right now.
	Ready bool `json:"ready"`
	// Missing names the pieces that are not installed yet, where they are what
	// stands between the host and a session.
	Missing []string `json:"missing,omitempty"`
	// Browser is the browser the session will run, as the host names it.
	Browser string `json:"browser,omitempty"`
	// Running reports whether the session is up right now.
	Running bool `json:"running"`
	// Active and Sub are systemd's own words for it, for the times when
	// "running" is not the whole story: activating, failed, dead.
	Active string `json:"active,omitempty"`
	Sub    string `json:"sub,omitempty"`
	// Port is where noVNC answers, and Geometry the size of the virtual screen.
	Port     int    `json:"port"`
	Geometry string `json:"geometry"`
	// Password is the VNC password. It is generated on the host and kept there;
	// Deployer reads it back so the screen can show it and put it in the link,
	// rather than asking somebody to type eight random characters on a phone.
	Password string `json:"password,omitempty"`
	// Homepage is the page the session opens when it starts.
	Homepage string `json:"homepage,omitempty"`
	// User is the account the session runs as — whose home the profile lives
	// in, and whose Downloads the files land in.
	User string `json:"user"`
	// Downloads is the directory a download ends up in, and Profile is where
	// the browser keeps the logins that make the next visit a tap.
	Downloads string `json:"downloads,omitempty"`
	Profile   string `json:"profile,omitempty"`
	// Files is the newest handful of downloads, so the screen can say the file
	// arrived rather than leaving somebody to go and look.
	Files []RemoteFile `json:"files"`
}

// RemoteFile is one file in the downloads directory.
type RemoteFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	// AgeS is how long ago it was written, in seconds, as the host counts.
	AgeS int64 `json:"ageS"`
}

// remotePieces is what a session needs on the host. openbox is deliberately not
// among them: without a window manager the browser still runs, its dialogs are
// just harder to move, and a missing package is no reason to refuse to start.
var remotePieces = []string{"Xvfb", "x11vnc", "websockify"}

// remoteBrowsers are the browsers a session will use, best first. Chromium is
// first because it is what a Raspberry Pi already has, and because its download
// directory can be set without a person clicking through a settings screen over
// VNC.
var remoteBrowsers = []string{"chromium", "chromium-browser", "google-chrome", "firefox-esr", "firefox"}

// geometryPattern is a screen size, checked before it reaches a command line.
var geometryPattern = regexp.MustCompile(`^([0-9]{3,4})x([0-9]{3,4})$`)

// MaxRemoteLogBytes is how much of the setup log comes back. apt is chatty and
// the end is the part worth reading.
const MaxRemoteLogBytes = 16 << 10

// remoteStatusScript answers everything about a session in one round trip:
// where setup got to, which pieces are installed, what the settings say, what
// systemd makes of the unit, and what is in the downloads directory.
//
// Nothing here writes, so it is safe to ask as often as a screen wants to.
const remoteStatusScript = `set -u
r=$1
u=$2
etc="$r` + remoteConfDir + `"

printf '@@state\n'
cat "$etc/setup.state" 2>/dev/null || printf 'absent\n'

printf '@@logsize\n'
wc -c < "$etc/setup.log" 2>/dev/null || printf '0\n'

printf '@@log\n'
tail -c %d "$etc/setup.log" 2>/dev/null | base64 2>/dev/null

printf '@@config\n'
cat "$etc/config" 2>/dev/null

printf '@@password\n'
cat "$etc/password" 2>/dev/null

printf '@@homepage\n'
cat "$etc/homepage" 2>/dev/null

printf '@@have\n'
for b in %s; do
  command -v "$b" >/dev/null 2>&1 && printf '%%s\n' "$b"
done

printf '@@unit\n'
if command -v systemctl >/dev/null 2>&1; then
  systemctl show --no-pager -p LoadState -p ActiveState -p SubState -- %s 2>/dev/null
fi

home=$(getent passwd "$u" 2>/dev/null | cut -d: -f6)
[ -n "$home" ] || home="/home/$u"
dl="$r$home/Downloads"
printf '@@downloads\n%%s\n' "$dl"
printf '@@profile\n%%s\n' "$r$home/.config/deployer-remote"

printf '@@files\n'
now=$(date +%%s 2>/dev/null || echo 0)
if [ -d "$dl" ]; then
  ls -1t -- "$dl" 2>/dev/null | head -8 | while IFS= read -r n; do
    [ -f "$dl/$n" ] || continue
    meta=$(stat -c '%%Y %%s' -- "$dl/$n" 2>/dev/null) || meta="0 0"
    printf '%%s %%s\t%%s\n' "$meta" "$now" "$n"
  done
fi

# Reading the state of a session never fails: every part of it is optional, and
# a piece that is not there is an answer rather than an error. What comes back
# from a host with none of this is "absent", which is what the screen offers to
# fix.
exit 0
`

// RemoteStatus reports on a host's remote session: what is installed, what the
// settings are, and whether it is running.
func (s *Service) RemoteStatus(ctx context.Context, h *store.Host) (*RemoteSession, error) {
	script := fmt.Sprintf(remoteStatusScript,
		MaxRemoteLogBytes, strings.Join(remoteBrowsersAndPieces(), " "), RemoteUnit)
	res, err := s.run(ctx, h, elevate(script, "", h.Username), "")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, failure(res, "could not read the remote session on "+h.Name)
	}
	return parseRemoteStatus(res.Stdout, h.Username), nil
}

// remoteBrowsersAndPieces is what the status script looks for on the PATH: the
// programs a session is made of, and every browser it would be willing to run.
func remoteBrowsersAndPieces() []string {
	return append(append([]string{}, remotePieces...), remoteBrowsers...)
}

// parseRemoteStatus turns the script's output into the one answer the screen
// works from. A host that has never been set up is not an error: it reads as a
// session that is absent, which is exactly what the screen offers to fix.
func parseRemoteStatus(out, user string) *RemoteSession {
	found := sections(out)
	session := &RemoteSession{
		Unit:      RemoteUnit,
		User:      user,
		Setup:     "absent",
		Port:      DefaultRemotePort,
		Geometry:  DefaultRemoteGeometry,
		Downloads: first(found["downloads"]),
		Profile:   first(found["profile"]),
		Files:     []RemoteFile{},
	}

	// "failed:4" carries the exit status the installer died on, which is the
	// difference between "apt could not be reached" and "there is no browser
	// to install".
	state := first(found["state"])
	if code, ok := strings.CutPrefix(state, "failed:"); ok {
		session.Setup = "failed"
		session.SetupExit, _ = strconv.Atoi(strings.TrimSpace(code))
	} else if state != "" {
		session.Setup = state
	}
	// The log arrives as a tail, so its first line is usually half of one. Told
	// how big the whole file was, decodeLog drops that half rather than showing
	// it as a line apt never wrote.
	if log, _ := decodeLog(found["log"], first(found["logsize"])); strings.TrimSpace(log) != "" {
		session.SetupLog = log
	}

	for _, line := range found["config"] {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "PORT":
			if port, err := strconv.Atoi(value); err == nil && port > 0 {
				session.Port = port
			}
		case "GEOMETRY":
			if geometryPattern.MatchString(value) {
				session.Geometry = value
			}
		}
	}

	session.Password = first(found["password"])
	session.Homepage = first(found["homepage"])

	have := map[string]bool{}
	for _, line := range found["have"] {
		have[strings.TrimSpace(line)] = true
	}
	for _, piece := range remotePieces {
		if !have[piece] {
			session.Missing = append(session.Missing, piece)
		}
	}
	for _, browser := range remoteBrowsers {
		if have[browser] {
			session.Browser = browser
			break
		}
	}
	if session.Browser == "" {
		session.Missing = append(session.Missing, "a browser")
	}

	props := map[string]string{}
	for _, line := range found["unit"] {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[key] = value
		}
	}
	session.Active = props["ActiveState"]
	session.Sub = props["SubState"]
	session.Running = session.Active == "active" || session.Active == "activating"

	// Ready means a start would work: every piece present, a password stored,
	// and a unit systemd has actually loaded.
	session.Ready = len(session.Missing) == 0 && session.Password != "" && props["LoadState"] == "loaded"
	session.Files = parseRemoteFiles(found["files"])
	return session
}

// parseRemoteFiles reads the downloads listing: "<mtime> <size> <now>\t<name>",
// newest first. The host does the arithmetic on its own clock, because the
// phone's and the Pi's rarely agree to the second.
func parseRemoteFiles(lines []string) []RemoteFile {
	files := []RemoteFile{}
	for _, line := range lines {
		meta, name, ok := strings.Cut(line, "\t")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			continue
		}
		mtime, _ := strconv.ParseInt(fields[0], 10, 64)
		size, _ := strconv.ParseInt(fields[1], 10, 64)
		now, _ := strconv.ParseInt(fields[2], 10, 64)
		age := now - mtime
		if age < 0 || now == 0 || mtime == 0 {
			age = 0
		}
		files = append(files, RemoteFile{Name: name, Size: size, AgeS: age})
		if len(files) == 5 {
			break
		}
	}
	return files
}

// RemoteSetup is what a caller may choose about a session. Everything else —
// the display number, the VNC port, where things are written — is Deployer's
// business and is not worth a field on a phone.
type RemoteSetup struct {
	// Geometry is the virtual screen's size, "1280x800" by default.
	Geometry string `json:"geometry"`
	// Port is where noVNC answers, 6080 by default.
	Port int `json:"port"`
	// Homepage is the page the browser opens with.
	Homepage string `json:"homepage"`
	// Reset throws away the stored VNC password and makes a new one.
	Reset bool `json:"reset"`
}

// remoteSetupScript writes everything a session is made of and then hands the
// slow half to the host to get on with.
//
// The two generated scripts and the unit file are written here and now, because
// they are deterministic and take no time. Installing the packages is neither:
// on a Pi, apt fetching a browser is minutes of work over a link that may be a
// phone's. So it is detached — nohup setsid, output to a log, the exit status
// recorded by an EXIT trap — for the same reason a self-update is: an SSH
// session that ends takes its command with it, and a phone that locks its
// screen must not be able to cancel an install half way through a package.
//
// What comes back from this call is only "it started". Where it got to is what
// the status script is for.
const remoteSetupScript = `set -u
r=$1
u=$2
geom=$3
port=$4
url=$5
reset=$6

if [ "$(id -u 2>/dev/null || echo 1)" != 0 ]; then
  printf 'setting up a remote session needs root\n' >&2
  exit 3
fi

etc="$r%[1]s"
lib="$r%[2]s"
units="$r/etc/systemd/system"
mkdir -p "$etc" "$lib" "$units" || { printf 'could not write to %%s\n' "$etc" >&2; exit 4; }
# The session runs as the SSH user and has to read the password x11vnc
# authenticates against, so the directory is theirs and nobody else's.
chown "$u" "$etc" 2>/dev/null || true
chmod 750 "$etc" 2>/dev/null || true

home=$(getent passwd "$u" 2>/dev/null | cut -d: -f6)
[ -n "$home" ] || home="/home/$u"
profile="$r$home/.config/deployer-remote"
dl="$r$home/Downloads"

cat > "$lib/remote-session.sh" <<'SESSION'
%[3]s
SESSION
chmod 755 "$lib/remote-session.sh" || exit 5

cat > "$lib/remote-install.sh" <<'INSTALL'
%[4]s
INSTALL
chmod 750 "$lib/remote-install.sh" || exit 5

# Settings are written as lines to be read, never as a file to be sourced: the
# session script is given them as arguments by systemd, and Deployer parses
# them back. Nothing on the host is fed to a shell that would run what is in it.
{
  printf 'PORT=%%s\n' "$port"
  printf 'GEOMETRY=%%s\n' "$geom"
  printf 'DISPLAY_NUM=%%s\n' %[5]d
  printf 'VNC_PORT=%%s\n' %[6]d
  printf 'DOWNLOADS=%%s\n' "$dl"
  printf 'PROFILE=%%s\n' "$profile"
} > "$etc/config" || exit 6
printf '%%s\n' "$url" > "$etc/homepage" || exit 6

# The unit has no [Install] section on purpose: systemd calls that static, so
# the session cannot be enabled and does not come back after a reboot. A
# logged-in browser should run for as long as somebody is using it and no
# longer.
cat > "$units/%[7]s" <<UNIT
[Unit]
Description=Deployer remote browser session
After=network-online.target

[Service]
Type=simple
User=$u
ExecStart=%[2]s/remote-session.sh $geom %[5]d %[6]d $port $etc $profile $dl
Restart=no
TimeoutStopSec=20
UNIT
[ $? -eq 0 ] || exit 6

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
fi

: > "$etc/setup.log" || exit 7
printf 'running\n' > "$etc/setup.state" || exit 7
nohup setsid sh "$lib/remote-install.sh" "$r" "$u" "$reset" >> "$etc/setup.log" 2>&1 < /dev/null &
printf 'started\n'
`

// remoteInstallScript is the half that takes minutes. It runs detached, so its
// only way of reporting anything is the log it writes and the state file its
// EXIT trap leaves behind — which is what the status script reads back.
//
// A browser is installed by trying the names distributions actually use, in
// order, rather than by asking what this one is called: Raspberry Pi OS has
// called Chromium both things within living memory, and a failed apt tells us
// the answer more reliably than a version check would.
const remoteInstallScript = `set -u
r=$1
u=$2
reset=$3
etc="$r%[1]s"

trap 'code=$?; if [ "$code" -eq 0 ]; then printf "ok\n" > "$etc/setup.state"; else printf "failed:%%s\n" "$code" > "$etc/setup.state"; fi' EXIT

export DEBIAN_FRONTEND=noninteractive
command -v apt-get >/dev/null 2>&1 || {
  printf 'this host has no apt-get, so Deployer cannot install the pieces itself\n'
  exit 9
}
printf '== updating the package list\n'
apt-get update || exit 10

printf '== installing the virtual screen, the VNC server and noVNC\n'
apt-get install -y xvfb x11vnc novnc websockify || exit 11

printf '== installing a window manager\n'
apt-get install -y openbox || printf 'openbox is not available here; dialogs will be unmanaged\n'

printf '== looking for a browser\n'
browser=""
for b in %[2]s; do
  if command -v "$b" >/dev/null 2>&1; then browser="$b"; break; fi
done
if [ -z "$browser" ]; then
  printf '== installing a browser\n'
  apt-get install -y chromium || apt-get install -y chromium-browser || apt-get install -y firefox-esr || exit 12
fi

if [ "$reset" = 1 ] || [ ! -s "$etc/vncpasswd" ]; then
  printf '== storing a VNC password\n'
  # Eight characters because that is all the VNC protocol carries: a longer one
  # would be silently cut, and half a password is worse than a short one. The
  # alphabet leaves out the characters that are read wrong off a screen.
  pw=$(tr -dc 'abcdefghijkmnpqrstuvwxyz23456789' < /dev/urandom 2>/dev/null | head -c 8)
  [ ${#pw} -eq 8 ] || exit 13
  x11vnc -storepasswd "$pw" "$etc/vncpasswd" >/dev/null 2>&1 || exit 14
  printf '%%s\n' "$pw" > "$etc/password" || exit 14
  chmod 600 "$etc/vncpasswd" "$etc/password" 2>/dev/null || true
  chown "$u" "$etc/vncpasswd" 2>/dev/null || true
fi

printf '== ready\n'
`

// remoteSessionScript is what the unit runs. systemd hands it its settings as
// arguments, so it reads no configuration of its own and evaluates nothing.
//
// Everything it starts stays in the unit's control group, which is what makes
// stopping the session one systemctl call: systemd kills the group, and the
// screen, the window manager, the VNC server, the web gateway and the browser
// go together.
const remoteSessionScript = `#!/bin/sh
# Written by Deployer. A virtual screen with a browser on it, offered over VNC.
set -u
geom=$1
dpy=$2
vnc=$3
web=$4
conf=$5
profile=$6
downloads=$7

export DISPLAY=":$dpy"
w=${geom%%x*}
h=${geom#*x}

mkdir -p "$profile" "$downloads" || exit 2

# Chromium asks where to save a file unless its profile says otherwise, and
# answering that dialog over VNC on a phone is the worst part of doing this by
# hand. Seeding the preference once, before the profile exists, is what makes a
# download land in the host's own Downloads directory without anybody clicking
# anything. An existing profile is left alone: by then it is the person's.
prefs="$profile/Default/Preferences"
if [ ! -e "$prefs" ]; then
  mkdir -p "$profile/Default" || exit 2
  printf '{"download":{"default_directory":"%%s","prompt_for_download":false},"savefile":{"default_directory":"%%s"}}\n' \
    "$downloads" "$downloads" > "$prefs" || exit 2
fi

url=$(cat "$conf/homepage" 2>/dev/null) || url=""
case "$url" in
  http://*|https://*) ;;
  *) url="about:blank" ;;
esac

Xvfb "$DISPLAY" -screen 0 "${w}x${h}x24" -nolisten tcp &
# The X server takes a moment to come up, and everything after this needs it.
# Waiting for its socket beats sleeping for a guess.
i=0
while [ ! -e "/tmp/.X11-unix/X$dpy" ] && [ "$i" -lt 100 ]; do
  i=$((i + 1))
  sleep 0.1
done

# Without a window manager the browser still runs; its dialogs are just stuck
# where they open. That is a degraded session, not a broken one, so a host
# without openbox carries on.
if command -v openbox >/dev/null 2>&1; then
  openbox &
fi

# -localhost is what keeps the VNC port off the network: the only way in is the
# gateway below, which is the port Deployer's link points at.
x11vnc -display "$DISPLAY" -rfbauth "$conf/vncpasswd" -rfbport "$vnc" \
  -localhost -forever -shared -noxdamage -quiet &

web_root=""
for d in /usr/share/novnc /usr/share/webapps/novnc /usr/share/novnc-common; do
  if [ -d "$d" ]; then web_root="$d"; break; fi
done
if [ -n "$web_root" ]; then
  websockify --web="$web_root" "$web" "localhost:$vnc" &
else
  websockify "$web" "localhost:$vnc" &
fi

browser=""
for b in %s; do
  if command -v "$b" >/dev/null 2>&1; then browser="$b"; break; fi
done
[ -n "$browser" ] || exit 3

case "$browser" in
  firefox*)
    set -- --profile "$profile" --width "$w" --height "$h" --new-window "$url"
    ;;
  *)
    # --password-store=basic matters on a machine with no desktop session:
    # without it Chromium waits for a keyring that is never going to answer.
    set -- --user-data-dir="$profile" --no-first-run --no-default-browser-check \
      --password-store=basic --disable-features=Translate \
      --window-position=0,0 --window-size="$w,$h" --start-maximized "$url"
    # Chromium refuses to start as root with its sandbox on, and a host whose
    # SSH user is root is a host where the session would otherwise never come
    # up at all. Everywhere else the sandbox stays exactly where it is.
    if [ "$(id -u 2>/dev/null || echo 1000)" = 0 ]; then
      set -- --no-sandbox "$@"
    fi
    ;;
esac

# A browser closed by a stray tap should not end the session — the screen, the
# VNC server and the gateway are all still there, and starting it again is
# cheaper than starting the session again.
while :; do
  "$browser" "$@" >/dev/null 2>&1
  sleep 2
done &

wait
`

// SetupRemote installs and configures a host's remote session. It returns as
// soon as the install is under way; RemoteStatus is how its progress is read.
//
// It is idempotent. Running it again rewrites the scripts and the unit, keeps
// the password unless asked to replace it, and leaves the browser profile — and
// so every site it is signed into — alone.
func (s *Service) SetupRemote(ctx context.Context, h *store.Host, opts RemoteSetup) (*RemoteSession, error) {
	geometry, err := cleanGeometry(opts.Geometry)
	if err != nil {
		return nil, err
	}
	port, err := cleanRemotePort(opts.Port)
	if err != nil {
		return nil, err
	}
	homepage, err := CleanRemoteURL(opts.Homepage)
	if err != nil {
		return nil, err
	}
	if !userPattern.MatchString(h.Username) {
		return nil, invalid("%q is not a user a session can run as", h.Username)
	}

	script := fmt.Sprintf(remoteSetupScript,
		remoteConfDir, remoteLibDir,
		fmt.Sprintf(remoteSessionScript, strings.Join(remoteBrowsers, " ")),
		fmt.Sprintf(remoteInstallScript, remoteConfDir, strings.Join(remoteBrowsers, " ")),
		remoteDisplay, remoteVNCPort, RemoteUnit)

	res, err := s.run(ctx, h, elevate(script,
		"", h.Username, geometry, strconv.Itoa(port), homepage, boolArg(opts.Reset)), "")
	if err != nil {
		return nil, err
	}
	if res.ExitCode == 3 {
		return nil, errNeedsRoot(h.Username, h.Name)
	}
	if res.ExitCode != 0 {
		return nil, failure(res, "could not set up a remote session on "+h.Name)
	}
	return s.RemoteStatus(ctx, h)
}

// remoteStartScript points the session at a page and starts it, in one round
// trip. Writing the page first is what makes "open this site" a single tap
// rather than a URL typed into a browser over VNC with a phone keyboard.
const remoteStartScript = `set -u
r=$1
url=$2
etc="$r%[1]s"
[ -f "$etc/config" ] || { printf 'this host has no remote session set up\n' >&2; exit 3; }
if [ -n "$url" ]; then
  printf '%%s\n' "$url" > "$etc/homepage" || exit 4
fi
command -v systemctl >/dev/null 2>&1 || { printf 'systemd is not installed on this host\n' >&2; exit 5; }
systemctl --no-pager start -- %[2]s || exit 6
printf 'started\n'
`

// StartRemote starts the session, optionally opening a page in it.
func (s *Service) StartRemote(ctx context.Context, h *store.Host, page string) (*RemoteSession, error) {
	homepage, err := CleanRemoteURL(page)
	if err != nil {
		return nil, err
	}
	script := fmt.Sprintf(remoteStartScript, remoteConfDir, RemoteUnit)
	res, err := s.run(ctx, h, elevate(script, "", homepage), "")
	if err != nil {
		return nil, err
	}
	if res.ExitCode == 3 {
		return nil, invalid("this host has no remote session set up yet")
	}
	if res.ExitCode != 0 {
		return nil, failure(res, "could not start the remote session on "+h.Name)
	}
	return s.RemoteStatus(ctx, h)
}

// StopRemote stops the session. systemd kills the unit's whole control group,
// so the browser, the VNC server and the gateway go with it — which is what
// makes stopping it worth doing between fetches rather than leaving a
// logged-in browser running all week.
func (s *Service) StopRemote(ctx context.Context, h *store.Host) (*RemoteSession, error) {
	if err := s.Act(ctx, h, RemoteUnit, "stop"); err != nil {
		return nil, err
	}
	return s.RemoteStatus(ctx, h)
}

// remoteRemoveScript takes the session back off the host: the unit first, so
// nothing is left pointing at a file that has gone, then what Deployer wrote.
//
// Two things it does not touch. The packages stay, because apt installed them
// and something else may want them. The downloads stay, because they are the
// files somebody went to the trouble of fetching. The browser profile goes only
// when asked, because it is where the logins are: throwing it away silently
// would mean signing in to everything again with no warning.
const remoteRemoveScript = `set -u
r=$1
u=$2
purge=$3
etc="$r%[1]s"
lib="$r%[2]s"
unit="$r/etc/systemd/system/%[3]s"

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop -- %[3]s >/dev/null 2>&1 || true
fi
rm -f -- "$unit" || exit 3
rm -rf -- "$etc" "$lib/remote-session.sh" "$lib/remote-install.sh" || exit 3
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl reset-failed -- %[3]s >/dev/null 2>&1 || true
fi

if [ "$purge" = 1 ]; then
  home=$(getent passwd "$u" 2>/dev/null | cut -d: -f6)
  [ -n "$home" ] || home="/home/$u"
  rm -rf -- "$r$home/.config/deployer-remote" || exit 4
fi
printf 'removed\n'
`

// RemoveRemote deletes the session from a host. purge takes the browser profile
// — and every site it is signed into — with it.
func (s *Service) RemoveRemote(ctx context.Context, h *store.Host, purge bool) error {
	if !userPattern.MatchString(h.Username) {
		return invalid("%q is not a user a session could have run as", h.Username)
	}
	script := fmt.Sprintf(remoteRemoveScript, remoteConfDir, remoteLibDir, RemoteUnit)
	res, err := s.run(ctx, h, elevate(script, "", h.Username, boolArg(purge)), "")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return failure(res, "could not remove the remote session from "+h.Name)
	}
	return nil
}

// RemoteURL is where the session is reached: noVNC, on the host, told to
// connect straight away at the size of whatever is looking at it.
//
// The password rides in the query string. It is a real trade-off and worth
// naming: it puts eight characters into a browser history on the LAN, and what
// it buys is not having to type them into a phone every time. The session is
// only reachable from the network the host is on, and anyone already on that
// network with Deployer open can read the password from this screen anyway.
func RemoteURL(address string, session *RemoteSession) string {
	if session == nil || session.Port == 0 || address == "" {
		return ""
	}
	query := url.Values{
		"autoconnect": {"1"},
		"resize":      {"scale"},
		"path":        {"websockify"},
	}
	if session.Password != "" {
		query.Set("password", session.Password)
	}
	return (&url.URL{
		Scheme:   "http",
		Host:     hostPort(address, session.Port),
		Path:     "/vnc.html",
		RawQuery: query.Encode(),
	}).String()
}

// hostPort joins an address and a port, bracketing an IPv6 literal.
func hostPort(address string, port int) string {
	if strings.Contains(address, ":") && !strings.HasPrefix(address, "[") {
		address = "[" + address + "]"
	}
	return address + ":" + strconv.Itoa(port)
}

// cleanGeometry checks a screen size. The bounds are not fussiness: a screen
// smaller than this lays sites out as if on a phone, which is what the session
// exists to avoid, and one larger costs the Pi memory it does not have.
func cleanGeometry(geometry string) (string, error) {
	geometry = strings.TrimSpace(strings.ToLower(geometry))
	if geometry == "" {
		return DefaultRemoteGeometry, nil
	}
	match := geometryPattern.FindStringSubmatch(geometry)
	if match == nil {
		return "", invalid("a screen size reads like 1280x800")
	}
	width, _ := strconv.Atoi(match[1])
	height, _ := strconv.Atoi(match[2])
	if width < 640 || width > 3840 || height < 480 || height > 2160 {
		return "", invalid("a screen between 640x480 and 3840x2160 is what this can offer")
	}
	return geometry, nil
}

// cleanRemotePort checks the port noVNC will answer on.
func cleanRemotePort(port int) (int, error) {
	if port == 0 {
		return DefaultRemotePort, nil
	}
	if port < 1024 || port > 65535 {
		return 0, invalid("a port between 1024 and 65535, please")
	}
	if port == remoteVNCPort {
		return 0, invalid("%d is the port the session's VNC server uses", remoteVNCPort)
	}
	return port, nil
}

// CleanRemoteURL checks the page a session opens with. It must be an ordinary
// web address: it reaches the browser as an argument, and a value that could be
// read as an option, a local file or a javascript: URL is not a page somebody
// asked for.
func CleanRemoteURL(page string) (string, error) {
	page = strings.TrimSpace(page)
	if page == "" {
		return "", nil
	}
	if len(page) > 2048 {
		return "", invalid("that address is too long")
	}
	if strings.ContainsAny(page, " \t\r\n\"'`\\") {
		return "", invalid("that does not look like a web address")
	}
	parsed, err := url.Parse(page)
	if err != nil || parsed.Host == "" {
		return "", invalid("that does not look like a web address")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", invalid("only http:// and https:// addresses can be opened")
	}
	return page, nil
}

// boolArg renders a flag as the "1" or "" the scripts test for.
func boolArg(on bool) string {
	if on {
		return "1"
	}
	return ""
}
