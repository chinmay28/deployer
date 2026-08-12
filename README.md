# Deployer

Deploys apps onto home servers, and watches them afterwards. Add a host by
address (`nakedpi.local` or `192.168.2.123`), point Deployer at an app's
one-line install command, and redeploy it later from your phone in one tap.

It runs on the same kind of box it manages: a single static Go binary that
serves both the REST API and a mobile-first PWA, SQLite for state, no runtime
dependencies.

## Install

On the machine you want to run Deployer on — a Raspberry Pi is the intended
home:

```sh
curl -fsSL https://raw.githubusercontent.com/chinmay28/deployer/main/scripts/quickstart.sh | sudo bash
```

Then open `http://<that machine>:8899` on your phone and add it to the home
screen.

Re-run the same command to upgrade. Upgrades build first and only then touch the
running service: the database is snapshotted, the new binary is health-checked
after it starts, and if it doesn't come up the previous binary and database are
restored automatically.

```sh
# a PIN for the web UI, a different port, a specific version
curl -fsSL .../quickstart.sh | sudo DEPLOYER_PIN=1234 DEPLOYER_PORT=9000 DEPLOYER_REF=v1.0 bash

# remove the service (your data and SSH key are kept)
curl -fsSL .../quickstart.sh | sudo bash -s -- --uninstall
```

## The home host

Deployer recognises the machine it runs on and registers it as a host, tagged
**Home**. Recognition is by `/etc/machine-id`, so it holds however you reach the
machine — `127.0.0.1`, a LAN address, or `nakedpi.local` all resolve to the same
identity.

The home host is an ordinary host in every other respect: Deployer still
connects to it over SSH, and still needs its key authorized and passwordless
sudo. That is deliberate. Running install commands directly would mean running
them inside Deployer's systemd sandbox, where `NoNewPrivileges=yes` blocks
`sudo` outright — going through SSH keeps the sandbox intact.

Delete the home host if you don't want it; a restart won't bring it back.

## Updating Deployer

Settings shows the running version with an **Update Deployer** button. It builds
from a git ref you can change at the confirmation step (branch, tag or commit),
and is recorded as a normal deployment with a full log.

The awkward part is that the update restarts Deployer half way through, killing
the process watching it — and, with it, the SSH session carrying the install
script, because `sshd` hangs up a remote command when its client disappears. So
a self-update on the home host runs **detached**: `nohup setsid`, output to a
file on the host, with the exit status recorded by an `EXIT` trap so an install
that ends in `exit 1` still reports one.

Deployer follows that file. When it is restarted by the update it hands the
deployment over rather than recording a verdict, and the process that comes back
picks the same file up, replays everything written while it was gone, and
finishes the record.

**The outage is expected, and treated as such.** While Deployer is down the UI
says "Can't reach Deployer — reconnecting…" and keeps showing the last state it
reported, rather than an error; the log page reconnects on its own; and health
checks are suppressed for any app with a deployment in flight, so Deployer does
not report itself as "Not responding" while it is the thing restarting. After a
deployment, a health check retries for a minute before calling anything
unhealthy, which is what a service that restarts needs.

## Adding a host

Deployer generates its own ed25519 keypair on first run and keeps it in its
database. There is no other key material to manage. A host needs two things
before Deployer can use it: that public key in the SSH user's
`authorized_keys`, and passwordless sudo for the user, since install commands
end in `| sudo bash`.

**The add-host form will do both for you.** Give it the SSH user's password and
Deployer signs in with it once, appends its own key, writes
`/etc/sudoers.d/deployer` through `sudo -S`, and then reconnects with the key
alone to prove it worked — reporting each step as it goes. Every step is
idempotent, so a partial run is simply repeated: hosts added earlier, or a
first attempt that failed, get the same thing from **Set up access** on the
host's page.

The password is used for that one connection and nothing else. It is never
written to the database, never logged, and never sent back to the browser; it
reaches the host over the SSH handshake and `sudo`'s stdin, never a command
line where the host's process list would show it. The sudoers file is checked
with `visudo` before it is moved into place, because a malformed drop-in breaks
`sudo` for everyone on the machine.

Leave the password empty and nothing changes from before: the host is saved and
you run the two commands from Settings on the machine yourself, signed in as the
user Deployer will connect as.

```sh
# 1. trust Deployer's key
mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo 'ssh-ed25519 AAAA... deployer' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys

# 2. allow unattended installs, since install scripts end in `| sudo bash`
echo "$(whoami) ALL=(ALL) NOPASSWD:ALL" | sudo tee /etc/sudoers.d/deployer >/dev/null && sudo chmod 440 /etc/sudoers.d/deployer
```

That is also the route for a host with password logins turned off, which is what
Deployer reports if the handshake is refused.

**Test connection** checks all of it at once — reachability, key auth,
passwordless sudo — and tells you which part is missing.

A host's SSH key is pinned the first time Deployer connects. If it ever changes,
connections fail loudly rather than quietly trusting the new one.

## Adding an app

An app is a name plus the one-line command that installs it:

```
curl -fsSL https://raw.githubusercontent.com/chinmay28/countroster/main/scripts/quickstart.sh | sudo bash
```

Anything you want to vary per deployment becomes a **parameter**, written as
`{{name}}` in the command. `{{host}}`, `{{hostname}}` and `{{user}}` are always
available and refer to the target host.

Parameter values are substituted as complete shell-quoted words, so a value can
never add commands of its own. That also means you must not put quotes around a
placeholder yourself — `--port {{port}}`, not `--port "{{port}}"` — and Deployer
rejects the app rather than silently producing literal quotes.

Install commands run with `pipefail`, so a failing `curl` in
`curl ... | sudo bash` fails the deployment instead of being masked by bash's
exit status, and with `DEBIAN_FRONTEND=noninteractive` so package managers don't
stop to ask questions.

An app can also declare a **health check** — an HTTP URL (`http://{{host}}:8787/`)
or a systemd unit (`countroster.service`) — which is what turns "the script ran"
into "the app is actually running". It runs after each deploy and every minute
after that.

Between the health check URL and a parameter called `port`, an app has usually
already said which **ports** it answers on, so Deployer reads them back out and
shows them wherever the app is listed: `on nakedpi · port 8787 · updated 3 min
ago`. Knowing an app is healthy is only half of knowing where to open it.
Nothing is scanned and nothing is guessed — an app that declares neither shows
no port at all.

## Deploying

Deploy picks a host, shows the parameters, and shows the exact command that will
run. The log streams live to your phone while it runs, and can be canceled.

A successful deployment records an installation, which is what makes the next
one a single tap: **Redeploy** reopens the same confirmation prefilled with the
parameters you used last time. Deployments of the same app to the same host are
serialized — a second one is refused while the first is running.

## Managing a host

A host's page has a **Manage** section for the jobs that otherwise mean finding
a laptop and an SSH client.

**Files** browses the machine. Tap a directory to descend, a file to read it,
and **Edit** to change it — a config, a unit file, a script. There is no agent
and no file transfer: a listing is one `find` (or a `stat` loop where busybox
has no `find -printf`), and a file's contents cross the wire base64-encoded so
nothing is lost to an encoding, a control byte, or a missing final newline.

Saving writes through a temporary file in the same directory and moves it into
place, so a reader sees either the old file or the new one, never half of each,
and a failure part way through leaves the original alone. An existing file keeps
its mode and its owner. A symlink is followed: editing `/etc/resolv.conf` edits
what it points at rather than replacing the link.

Two things are deliberately not editable. A file over 512 KB comes back
truncated, and saving the part you can see would throw the rest away; a binary
file is shown as binary rather than run through a textarea that would mangle it.

**Services** is every systemd service and timer someone installed on the
machine by hand — the unit files under `/etc/systemd/system` and
`/usr/local/lib/systemd/system`. The distribution's own hundreds, in
`/usr/lib/systemd/system`, are not what you pick up a phone to fix, so they are
left out; where a unit file lives is the same rule systemd itself uses to decide
who wins. Filter chips cut the list four ways and put failures first.

Timers are listed beside the services because a scheduled job is written as a
pair — a `.service` with no `[Install]` section and a `.timer` that starts it —
and the timer is the half that carries the schedule and the half that gets
enabled. Listing only services showed such a job as a unit nothing appeared to
start, and a timer-only install, where the thing being scheduled is the
distribution's, showed nothing at all. A timer's row says when it next fires
rather than how long it has been waiting, and its screen says what it starts
instead of a memory figure and a PID it does not have. Sockets, paths and
targets are still left alone: naming them is one thing, and Deployer names the
ones that matter, but managing them from a phone is another.

Tapping one opens what it is doing — running, failed and why, for how long, its
memory and its PID — with **Start**, **Stop**, **Restart** and **Reload**, a
toggle for whether it comes back after a reboot, and the tail of its journal at
50, 200 or 1000 lines. Deployer waits for `systemctl` to finish rather than
returning as soon as the command is sent, so a service that takes half a minute
to come up takes half a minute to answer.

A unit with no `[Install]` section has nothing to enable — systemd calls it
`static`, and it runs only when something else pulls it in. Rather than leave
that as "started by another unit", the screen asks systemd which one and names
it: the socket, timer or path unit that activates it, or whatever wants,
requires, binds to or upholds it, most specific first. Services among them link
to their own screen. `PartOf` and `Requisite` are left out, since the first only
propagates stop and restart and the second refuses to start rather than
starting. systemd only names units it has loaded, so nothing named means nothing
is pulling it in right now — which the screen says in those words rather than
claiming nothing ever will.

**Add a service** writes a service from six fields — what to run, as whom, from
where, and what to do when it stops — rather than from a blank unit file, with
the file it is about to write on screen the whole time for anyone who would
rather type it. systemd is what validates it: the file is written, systemd is
told to read it, and anything it refuses to load is taken straight back off the
disk, because a unit file in /etc/systemd/system that systemd cannot read is
worse than no file at all. It is created stopped, and starting and enabling it
are separate steps, so a service that will not start says exactly that instead
of looking like an install that half happened. A name systemd already knows is
refused: a unit of the same name in /etc/systemd/system does not replace the
distribution's, it shadows it, and doing that to sshd by accident from a phone
is not a mistake worth leaving available.

**Deleting** takes the unit file, the symlinks that start it at boot, and the
drop-in overrides that are meaningless without it, then clears the failed state
so a service that died on its way out does not haunt `systemctl --failed`.
Whatever the service actually ran is left where it is. Two things it will not
do: delete a service that is still running — stopping it is a decision the
person deleting it makes, because deleting the unit of a running process leaves
it up with nothing left to describe or stop it — and delete anything outside an
administrator's own unit directories, since /usr/lib belongs to the package
manager.

The unit file is editable on the same screen, and saving runs
`systemctl daemon-reload` straight afterwards — a unit file edited and not
reloaded is a change that has not happened, which is the most common way an edit
from a phone appears to do nothing. The service keeps running the old settings
until it is restarted, and the screen says so.

The state comes from one `systemctl show` per screen: key=value lines that parse
the same on every version worth supporting, and unlike `status` it never wraps,
colours or truncates what it says. None of these screens poll — each is an SSH
session, so they refresh when you ask.

**Scheduled jobs** edits the crontab, the whole file at once, the way
`crontab -e` does — for the user Deployer signs in as, and for root. Cron is
what validates it: a crontab it refuses to parse is not installed, the old one
stays, and its complaint is what you see. A user with no crontab yet is an empty
editor, not an error.

**Power** reboots or shuts the machine down. The command is scheduled a few
seconds ahead and detached, so Deployer gets a clean answer instead of the
connection dying mid-command and having to guess what that meant — the same
reason a self-update runs detached. The host is marked offline as soon as it
accepts, because it is about to be.

All of this runs as root wherever the SSH user has passwordless sudo, and as
that user where it doesn't; which one it was is shown on the screen. That is not
an escalation — Deployer already holds a key that can run anything on the
machine — but a file browser that could not open `/etc` would be hiding the
reality rather than limiting it.

## Why it restarted

A Raspberry Pi that reboots on its own is the one question a phone is worst at
answering, because the answer is never in one place. **Why it restarted** reads
all of them in a single SSH session and says what it thinks, with the evidence
under it.

There are four places to read. `wtmp` remembers when the machine came up and —
the valuable part — whether anything recorded a shutdown before it, which is the
one signal that survives on a host that keeps no logs at all. The end of the
previous boot's journal, at any priority, is where an orderly shutdown shows
itself. That same boot's warnings and worse are where a kernel panic, an
out-of-memory kill, a critical temperature, a soft lockup, an SD card giving up
and the Pi's under-voltage warning all are; filtering by priority is what makes
reading hours of log affordable on a Pi. And `vcgencmd get_throttled` is the
only place a Raspberry Pi records its supply having sagged.

The verdict is a guess and is labelled as one. It tells a restart something asked
for from a panic, a lock-up, an out-of-memory kill, a thermal shutdown, an
under-voltage, a storage failure and a plain power cut — and, often, from "it
went down without saying why", which is an honest answer and a common one.
Confidence reads **the machine said so** only where the machine said so in as
many words; everything inferred is at best the best explanation available. Each
sign keeps the log line it was found in, because a verdict nobody can check is
not worth much on a screen this size.

**When a line was logged matters as much as what it says.** A machine that ran
out of memory at nine and restarted at three did not restart because it ran out
of memory. Every sign carries how many seconds before the restart it was
written, and only the ones inside two minutes of it are allowed to be the cause.
The rest are still shown — as the weather rather than the event.

The most useful finding is often an absence. A machine that was asked to restart
says so at length; a machine that lost power says nothing at all and its log
stops mid-sentence, which cannot be told apart from a lock-up so complete the
kernel never got to write about it. Deployer says that instead of picking the
more dramatic of the two. It also counts: one unexplained restart is bad luck,
and four in a week is the reason someone opened the screen.

**A journal kept in memory is the commonest reason there is no answer at all.**
Debian, and so Raspberry Pi OS, leaves systemd's log in memory unless
`/var/log/journal` exists — so the log of the boot that crashed dies with the
boot that crashed. Where that is the case the screen says so in those words and
offers to fix it, since creating that directory is the whole of the fix. Failing
that, the lines immediately before the last kernel banner in `/var/log/syslog`
are exactly the end of the previous boot, and are read instead.

## Monitoring

No agent is installed on the host. Deployer opens an SSH session and reads
`/proc/stat`, `/proc/meminfo`, `/proc/loadavg`, `df` and the thermal zone in a
single round trip, sampling `/proc/stat` twice a second apart so CPU usage is a
real interval average rather than an average since boot.

Hosts are polled every 30s in the background and every 5s while you have a host
open. Samples are kept for 24 hours.

**A number on its own does not say whether it is normal.** 40% CPU means one
thing on a machine that idles at 5% and another on one that sits at 35% all day,
so a host shows the range its CPU and memory moved through over the last 24
hours with the average marked inside it. The band is the day; the tick is the
mean. That is the whole of the retention window, so it is everything Deployer
knows about the machine. The arithmetic is done in SQL and only the six numbers
are sent — a phone polling every few seconds never carries a day of samples to
work them out.

## Security

Deployer holds a key that can run commands as root on every host you add. Treat
it accordingly:

- **Keep it on your LAN or Tailscale network.** Don't expose it to the internet.
- Anyone who can reach the UI can **browse and edit any file on every host**, as
  root, and restart the machines. That follows from the key Deployer already
  holds rather than adding to it, but it does put a root shell's reach behind a
  web page — which is the whole argument for the PIN and for the LAN.
- It runs **unauthenticated by default**. Set a PIN with `-pin` /
  `DEPLOYER_PIN` if anything less trusted can reach it.
- The **database contains the SSH private key**. The installer keeps it at mode
  700 under `/var/lib/deployer`; back it up like a secret.
- A **host password given during setup is never stored** — not in the database,
  not in the log. It exists for one request. It does cross the network to
  Deployer in the clear if you are on plain `http`, so set a host up from a
  network you trust, the same one you would type the PIN over.
- The systemd unit runs as a dedicated non-root user with `NoNewPrivileges`,
  `ProtectSystem=strict`, an empty capability set and a system-call filter.
- Rotating the key in Settings invalidates every host until you install the new
  public key on each one.

## Development

```sh
make build          # PWA into the Go embed directory, then the binary
make run            # build and start on :8899
make test           # Go tests, including SSH integration tests where sshd exists
make test-installer # install, upgrade, rollback and uninstall, in a sandbox
make test-provision # set a host up over SSH for real (root; changes the machine)

cd apps/web && npm run dev   # Vite dev server, proxying /api to :8899

make version        # print the version this tree would build as
```

### Versioning

The version is `vMAJOR.MINOR.PATCH`, where the patch number is the repository's
commit count — every commit is a patch release, so `v0.1.42` is the 42nd commit
on the 0.1 line. Major and minor are constants in
`server/internal/version/version.go`, bumped by hand.

The patch number only exists at build time, so `scripts/version.mjs` works it
out once and both halves of the build take it from there: the linker stamps it
into the binary, Vite inlines it into the bundle. The app header shows what the
PWA was built as, Settings and `/api/health` show what the binary was, and they
agree because a build makes both together.

A version ending in `.0` is an unstamped build rather than a release — a bare
`go build`, or a build where git couldn't be asked. That includes a **shallow**
clone, which is why the installer clones with `--filter=blob:none` instead of
`--depth 1`: cheap, but with the whole commit graph.

Server flags:

| Flag    | Env             | Default            | Meaning                    |
| ------- | --------------- | ------------------ | -------------------------- |
| `-addr` | `DEPLOYER_ADDR` | `:8899`            | listen address             |
| `-db`   | `DEPLOYER_DB`   | `data/deployer.db` | SQLite path                |
| `-pin`  | `DEPLOYER_PIN`  | _(empty)_          | optional PIN; empty = open |
| `-v`    |                 | `false`            | verbose logging            |
| `-self-user` | `DEPLOYER_SELF_USER` | _(the account Deployer runs as)_ | SSH user for the home host |
| `-self-repo` | `DEPLOYER_REPO` | `chinmay28/deployer` | repository a self-update builds from |
| `-self-ref`  | `DEPLOYER_REF`  | `main`             | default ref for a self-update |

The installer sets `DEPLOYER_SELF_USER` to whoever ran it, since the service
account is a `nologin` user and cannot be SSHed to.

Tests cover the probe parser against realistic and degraded `/proc` output, the
store, the API handlers, and — where a local `sshd` can be started — real SSH
connections: key auth, host-key pinning and rejection, live deployments, exit
codes, cancellation, log streaming and health checks.

The shell scripts behind host management are tested by running them: against a
real filesystem, through a real shell, with the parsers the SSH path uses, both
with GNU `find` and with it forced to fail so the busybox fallback is covered.
`systemctl` and `journalctl` are stood in for by scripts that answer the way the
real ones do, so the listing, the parsing, the fallback for a `journalctl` too
old to know `--no-hostname`, and what creating and deleting a service leave on
disk are all exercised without a running systemd.

The restart diagnosis gets the same treatment, with `PATH` replaced rather than
extended: half of what it has to get right is what it does when a command is
*not* there, and a stub cannot express absence. So the journal that answers, the
journal that has nothing from before the restart, the `last` too old to print
ISO timestamps, the rsyslog file read in its place and the host with none of
them are each a real run of the real script. The verdict itself is tested
against fixtures of every cause, including the two that matter most: a sign far
enough from the restart that it cannot be the cause, and a log that simply stops. The refusals get their own
tests, because they are the point: writing over an existing unit file, taking a
name systemd already knows, deleting a service that is still running, and
deleting anything under /usr/lib all have to fail.

The installer gets the same treatment in a sandbox: install, upgrade, the
rollback a failed upgrade is supposed to trigger, uninstall, and the Go
toolchain it fetches to build with — a download that is refused, one that
returns nothing at all, and one that works. The toolchain already on the
machine has to survive the first two, because an installer that removes it
before it has something to put in its place leaves the host unable to build
anything and every later run failing on a different line than the one that
broke.
Quoting is tested the same way — every path a person could type is handed to
`/bin/sh` and has to come back out the other side unchanged.

## API

| Method   | Path                                | Purpose                              |
| -------- | ----------------------------------- | ------------------------------------ |
| `GET`    | `/api/health`                       | liveness, and the running version    |
| `GET`    | `/api/overview`                     | everything the dashboard needs       |
| `GET`    | `/api/session`                      | whether a PIN is required            |
| `POST`   | `/api/session`                      | exchange a PIN for a session cookie  |
| `GET`    | `/api/settings/ssh`                 | public key and host setup commands   |
| `POST`   | `/api/settings/ssh/rotate`          | new keypair (re-authorize all hosts) |
| `GET`    | `/api/hosts`                        | hosts, each with its latest sample   |
| `POST`   | `/api/hosts`                        | add a host                           |
| `GET`    | `/api/hosts/{id}`                   | one host                             |
| `PATCH`  | `/api/hosts/{id}`                   | edit name, address, port or user     |
| `DELETE` | `/api/hosts/{id}`                   | remove a host and its history        |
| `POST`   | `/api/hosts/{id}/test`              | check reachability, key auth, sudo   |
| `POST`   | `/api/hosts/{id}/provision`         | one-time password setup, not stored  |
| `GET`    | `/api/hosts/{id}/metrics`           | samples, `?minutes=` up to 1440      |
| `POST`   | `/api/hosts/{id}/reboot`            | restart the machine (there is no shutdown) |
| `GET`    | `/api/hosts/{id}/boot`              | why it last restarted, and the evidence |
| `POST`   | `/api/hosts/{id}/boot/journal`      | keep the journal across restarts, so the next one is explainable |
| `GET`    | `/api/hosts/{id}/cron`              | a crontab, `?user=` (default: the SSH user) |
| `PUT`    | `/api/hosts/{id}/cron`              | install a crontab; cron validates it |
| `GET`    | `/api/hosts/{id}/services`          | hand-installed services and timers, and their state |
| `POST`   | `/api/hosts/{id}/services`          | write a new unit; systemd validates it |
| `DELETE` | `/api/hosts/{id}/services`          | delete a stopped service, `?name=`   |
| `GET`    | `/api/hosts/{id}/services/unit`     | one service or timer, `?name=`       |
| `GET`    | `/api/hosts/{id}/services/logs`     | its journal, `?name=&lines=` (20–2000) |
| `POST`   | `/api/hosts/{id}/services/action`   | start, stop, restart, reload, enable or disable |
| `POST`   | `/api/hosts/{id}/services/reload`   | `daemon-reload` after a unit file changes |
| `GET`    | `/api/hosts/{id}/files`             | list a directory, `?path=` (default: home) |
| `DELETE` | `/api/hosts/{id}/files`             | delete `?path=`, `&recursive=true` for a full directory |
| `GET`    | `/api/hosts/{id}/files/content`     | read a file, `?path=`                |
| `PUT`    | `/api/hosts/{id}/files/content`     | write a file, keeping mode and owner |
| `POST`   | `/api/hosts/{id}/files/mkdir`       | create a directory                   |
| `POST`   | `/api/hosts/{id}/files/rename`      | move a file, never over an existing one |
| `GET`    | `/api/apps`                         | apps                                 |
| `POST`   | `/api/apps`                         | add an app                           |
| `GET`    | `/api/apps/{id}`                    | one app                              |
| `PATCH`  | `/api/apps/{id}`                    | edit an app                          |
| `DELETE` | `/api/apps/{id}`                    | delete an app and its history        |
| `POST`   | `/api/apps/{id}/deploy`             | deploy to a host                     |
| `GET`    | `/api/installations`                | what is deployed where, with health  |
| `POST`   | `/api/installations/{id}/redeploy`  | run again with the saved parameters  |
| `POST`   | `/api/installations/{id}/check`     | run the health check now             |
| `DELETE` | `/api/installations/{id}`           | forget it (nothing is uninstalled)   |
| `GET`    | `/api/deployments`                  | history, `?appId=`/`?hostId=`        |
| `GET`    | `/api/deployments/{id}`             | one deployment, with its log         |
| `GET`    | `/api/deployments/{id}/stream`      | live log as server-sent events       |
| `POST`   | `/api/deployments/{id}/cancel`      | stop a running deployment            |
| `GET`    | `/api/self`                         | version, home host, update readiness |
| `POST`   | `/api/self/update`                  | update Deployer on the home host     |

## Layout

```
server/
  cmd/deployer/      entrypoint: flags, wiring, graceful shutdown
  cmd/icongen/       draws the app icons
  internal/store/    SQLite schema, append-only migrations, queries
  internal/sshx/     Deployer's keypair and SSH connections
  internal/metrics/  the agentless /proc probe and its parser
  internal/hosts/    connect, test, and poll hosts
  internal/hostops/  managing a host: files, services, crontab, power state,
                     and guessing why it last restarted
  internal/selfhost/ recognising this machine, and the app that updates it
  internal/deploy/   command rendering, the deployment runner, health checks
  internal/api/      REST handlers, SSE log stream, optional PIN gate
  internal/version/  the version number, and where MAJOR.MINOR is declared
  internal/web/      serves the embedded PWA
apps/web/            the PWA: React, Vite, no UI framework
scripts/             the installer, its test harness, and version.mjs
```
