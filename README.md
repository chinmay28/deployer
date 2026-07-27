# Deployer

Deploys apps onto home servers, and watches them afterwards. Add a host by
address (`nakedpi.local` or `192.168.2.123`), point Deployer at an app's
one-line install command, and redeploy it later from your phone in one tap.

Built to run on the same kind of box it manages: a single static Go binary that
serves both the REST API and a mobile-first PWA, with SQLite for state and no
runtime dependencies.

## Status

**M1 — hosts, SSH and monitoring: done.** The server manages hosts, connects to
them with its own SSH key, and collects telemetry.

Still to come: apps and deployments with live log streaming (M2), the PWA
itself (M3), and a `quickstart.sh` so Deployer can install itself (M4).

## How it connects to a host

Deployer generates its own ed25519 keypair on first run and stores it in its
database. There is no other key material to manage: you add the public key to a
host's `authorized_keys`, and grant that user passwordless sudo so install
scripts that end in `| sudo bash` can run unattended.

`GET /api/settings/ssh` returns the public key along with the two commands to
paste on a new host:

```sh
# 1. trust Deployer's key
mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo 'ssh-ed25519 AAAA... deployer' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys

# 2. allow unattended installs
echo "$(whoami) ALL=(ALL) NOPASSWD:ALL" | sudo tee /etc/sudoers.d/deployer >/dev/null && sudo chmod 440 /etc/sudoers.d/deployer
```

`POST /api/hosts/{id}/test` checks all of it at once — reachability, key auth,
sudo — and returns a hint for whatever is missing.

A host's SSH key is pinned the first time Deployer connects. If it later changes,
connections fail loudly rather than falling back to trusting it.

## Monitoring

No agent is installed on the host. Deployer opens an SSH session and reads
`/proc/stat`, `/proc/meminfo`, `/proc/loadavg`, `df` and the thermal zone in a
single round trip, sampling `/proc/stat` twice a second apart so CPU usage is a
real interval average rather than an average since boot.

Hosts are polled every 30s in the background, and every 5s while a host's page is
open in the UI. Samples are kept for 24 hours.

## Security posture

Deployer holds a key that can run root commands on your hosts. It is built for a
LAN and a Tailscale network, and runs unauthenticated by default. A PIN gate is
available if you want one:

```sh
deployer -pin 1234    # or DEPLOYER_PIN=1234
```

Do not expose it to the public internet.

## Running it

```sh
make test      # unit tests, plus SSH integration tests where sshd is available
make run       # builds and starts on :8899
```

| Flag    | Env             | Default            | Meaning                        |
| ------- | --------------- | ------------------ | ------------------------------ |
| `-addr` | `DEPLOYER_ADDR` | `:8899`            | listen address                 |
| `-db`   | `DEPLOYER_DB`   | `data/deployer.db` | SQLite path                    |
| `-pin`  | `DEPLOYER_PIN`  | _(empty)_          | optional PIN; empty = open     |
| `-v`    |                 | `false`            | verbose logging                |

The database contains Deployer's SSH private key. Keep it at mode 600 and back
it up like a secret.

## API

| Method   | Path                        | Purpose                              |
| -------- | --------------------------- | ------------------------------------ |
| `GET`    | `/api/health`               | liveness                             |
| `GET`    | `/api/session`              | whether a PIN is required            |
| `POST`   | `/api/session`              | exchange a PIN for a session cookie  |
| `GET`    | `/api/settings/ssh`         | public key and host setup commands   |
| `POST`   | `/api/settings/ssh/rotate`  | new keypair (re-authorize all hosts) |
| `GET`    | `/api/hosts`                | hosts, each with its latest sample   |
| `POST`   | `/api/hosts`                | add a host                           |
| `GET`    | `/api/hosts/{id}`           | one host                             |
| `PATCH`  | `/api/hosts/{id}`           | edit name, address, port or user     |
| `DELETE` | `/api/hosts/{id}`           | remove a host and its history        |
| `POST`   | `/api/hosts/{id}/test`      | check reachability, key auth, sudo   |
| `GET`    | `/api/hosts/{id}/metrics`   | samples, `?minutes=` up to 1440      |

## Layout

```
server/
  cmd/deployer/      entrypoint: flags, wiring, graceful shutdown
  internal/store/    SQLite schema, append-only migrations, queries
  internal/sshx/     Deployer's keypair and SSH connections
  internal/metrics/  the agentless /proc probe and its parser
  internal/hosts/    connect, test, and poll hosts
  internal/api/      REST handlers, optional PIN gate
  internal/web/      serves the embedded PWA
apps/web/            the PWA (M3)
scripts/             quickstart installer (M4)
```
