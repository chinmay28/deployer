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
database. There is no other key material to manage. Settings shows the public
key and the two commands to paste on the new machine, signed in as the user
Deployer will connect as:

```sh
# 1. trust Deployer's key
mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo 'ssh-ed25519 AAAA... deployer' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys

# 2. allow unattended installs, since install scripts end in `| sudo bash`
echo "$(whoami) ALL=(ALL) NOPASSWD:ALL" | sudo tee /etc/sudoers.d/deployer >/dev/null && sudo chmod 440 /etc/sudoers.d/deployer
```

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

## Deploying

Deploy picks a host, shows the parameters, and shows the exact command that will
run. The log streams live to your phone while it runs, and can be canceled.

A successful deployment records an installation, which is what makes the next
one a single tap: **Redeploy** reopens the same confirmation prefilled with the
parameters you used last time. Deployments of the same app to the same host are
serialized — a second one is refused while the first is running.

## Monitoring

No agent is installed on the host. Deployer opens an SSH session and reads
`/proc/stat`, `/proc/meminfo`, `/proc/loadavg`, `df` and the thermal zone in a
single round trip, sampling `/proc/stat` twice a second apart so CPU usage is a
real interval average rather than an average since boot.

Hosts are polled every 30s in the background and every 5s while you have a host
open. Samples are kept for 24 hours.

## Security

Deployer holds a key that can run commands as root on every host you add. Treat
it accordingly:

- **Keep it on your LAN or Tailscale network.** Don't expose it to the internet.
- It runs **unauthenticated by default**. Set a PIN with `-pin` /
  `DEPLOYER_PIN` if anything less trusted can reach it.
- The **database contains the SSH private key**. The installer keeps it at mode
  700 under `/var/lib/deployer`; back it up like a secret.
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
| `GET`    | `/api/hosts/{id}/metrics`           | samples, `?minutes=` up to 1440      |
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
  internal/selfhost/ recognising this machine, and the app that updates it
  internal/deploy/   command rendering, the deployment runner, health checks
  internal/api/      REST handlers, SSE log stream, optional PIN gate
  internal/version/  the version number, and where MAJOR.MINOR is declared
  internal/web/      serves the embedded PWA
apps/web/            the PWA: React, Vite, no UI framework
scripts/             the installer, its test harness, and version.mjs
```
