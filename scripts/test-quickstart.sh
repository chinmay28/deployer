#!/usr/bin/env bash
#
# Exercises quickstart.sh in a sandbox: fresh install, upgrade, and the
# rollback that a failed upgrade is supposed to trigger.
#
#   sudo ./scripts/test-quickstart.sh
#
# systemd is stubbed out (the unit file is still written and parsed, and the
# real binary is really started), so this runs anywhere Deployer builds.
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SANDBOX="$(mktemp -d)"
PORT="${TEST_PORT:-8901}"
trap 'cleanup' EXIT

cleanup() {
  if [ -f "$SANDBOX/stub/pid" ]; then
    kill "$(cat "$SANDBOX/stub/pid")" 2>/dev/null || true
  fi
  rm -rf "$SANDBOX"
}

pass() { printf '\033[1;32mPASS\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31mFAIL\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[1;35m--\033[0m %s\n' "$*"; }

[ "$(id -u)" -eq 0 ] || fail "This test needs root (it writes a unit file and starts a service)."

# ------------------------------------------------------------------ the stubs

mkdir -p "$SANDBOX/stub" "$SANDBOX/bin"

# A stand-in for systemctl that really starts and stops the binary named in the
# unit file, so the installer's health check and rollback are genuinely tested.
cat > "$SANDBOX/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
STUB_DIR="$STUB_DIR"
UNIT_FILE="$DEPLOYER_UNIT"

stop_it() {
  if [ -f "$STUB_DIR/pid" ]; then
    kill "$(cat "$STUB_DIR/pid")" 2>/dev/null || true
    rm -f "$STUB_DIR/pid"
  fi
}

start_it() {
  # STUB_FAIL_START simulates a new build that dies on startup.
  if [ "${STUB_FAIL_START:-0}" = 1 ]; then
    echo "stub: refusing to start (simulated failure)" >> "$STUB_DIR/journal"
    return 0
  fi
  local exec_start
  exec_start="$(sed -n 's/^ExecStart=//p' "$UNIT_FILE")"
  # shellcheck disable=SC2086
  setsid nohup $exec_start >> "$STUB_DIR/journal" 2>&1 < /dev/null &
  echo $! > "$STUB_DIR/pid"
}

case "${1:-}" in
  daemon-reload|enable|disable) [ "${2:-}" = "--now" ] && stop_it; exit 0 ;;
  stop)    stop_it ;;
  start)   start_it ;;
  restart) stop_it; sleep 0.2; start_it ;;
  *)       exit 0 ;;
esac
STUB

# The stub needs those two paths baked in; it runs without the caller's env.
sed -i "s|STUB_DIR=\"\$STUB_DIR\"|STUB_DIR=\"$SANDBOX/stub\"|" "$SANDBOX/bin/systemctl"
sed -i "s|UNIT_FILE=\"\$DEPLOYER_UNIT\"|UNIT_FILE=\"$SANDBOX/deployer.service\"|" "$SANDBOX/bin/systemctl"

cat > "$SANDBOX/bin/journalctl" <<STUB
#!/usr/bin/env bash
tail -n 25 "$SANDBOX/stub/journal" 2>/dev/null || true
STUB

chmod +x "$SANDBOX/bin/systemctl" "$SANDBOX/bin/journalctl"
export PATH="$SANDBOX/bin:$PATH"

# A local clone stands in for GitHub.
git clone --quiet "$REPO_ROOT" "$SANDBOX/origin.git" 2>/dev/null ||
  fail "could not clone the working tree"

# run_installer [VAR=VALUE ...] [-- script args ...]
run_installer() {
  local extra_env=()
  while [ $# -gt 0 ] && [ "$1" != "--" ]; do
    extra_env+=("$1")
    shift
  done
  [ "${1:-}" = "--" ] && shift

  env \
    DEPLOYER_REPO="$SANDBOX/origin.git" \
    DEPLOYER_REF="$(git -C "$SANDBOX/origin.git" rev-parse --abbrev-ref HEAD)" \
    DEPLOYER_INSTALL_DIR="$SANDBOX/opt" \
    DEPLOYER_DATA_DIR="$SANDBOX/data" \
    DEPLOYER_UNIT="$SANDBOX/deployer.service" \
    DEPLOYER_SERVICE_USER=root \
    DEPLOYER_PORT="$PORT" \
    STUB_DIR="$SANDBOX/stub" \
    "${extra_env[@]}" \
    bash "$REPO_ROOT/scripts/quickstart.sh" "$@"
}

# --------------------------------------------------------------- fresh install

info "Fresh install"
run_installer > "$SANDBOX/install.log" 2>&1 || {
  cat "$SANDBOX/install.log"
  fail "fresh install exited non-zero"
}

[ -x "$SANDBOX/opt/deployer" ] || fail "binary not installed"
[ -f "$SANDBOX/deployer.service" ] || fail "unit file not written"
grep -q "ExecStart=$SANDBOX/opt/deployer -addr :$PORT" "$SANDBOX/deployer.service" ||
  fail "unit file has the wrong ExecStart"
grep -q 'NoNewPrivileges=yes' "$SANDBOX/deployer.service" || fail "unit file lost its hardening"
curl -fsS --max-time 3 "http://127.0.0.1:$PORT/api/health" >/dev/null ||
  fail "the installed service is not answering"
pass "installs, writes a hardened unit, and the service answers"

# The database must be private: it holds Deployer's SSH private key.
perms="$(stat -c '%a' "$SANDBOX/data")"
[ "$perms" = "700" ] || fail "data directory is mode $perms, want 700"
pass "data directory is not world-readable"

# ---------------------------------------------------------------- add some data

curl -fsS --max-time 5 -X POST "http://127.0.0.1:$PORT/api/hosts" \
  -H 'Content-Type: application/json' \
  -d '{"name":"upgrade-canary","address":"10.0.0.9","username":"pi"}' >/dev/null ||
  fail "could not create a host to survive the upgrade"
pass "created a host before upgrading"

# --------------------------------------------------------------------- upgrade

info "Upgrade over the existing install"
run_installer > "$SANDBOX/upgrade.log" 2>&1 || {
  cat "$SANDBOX/upgrade.log"
  fail "upgrade exited non-zero"
}

grep -q 'Snapshotting the database' "$SANDBOX/upgrade.log" || fail "no database snapshot was taken"
ls "$SANDBOX/data/backups"/deployer-*.db >/dev/null 2>&1 || fail "snapshot file missing"
curl -fsS --max-time 3 "http://127.0.0.1:$PORT/api/hosts" | grep -q upgrade-canary ||
  fail "data did not survive the upgrade"
[ ! -e "$SANDBOX/opt/deployer.prev" ] || fail "the previous binary was left behind"
pass "upgrades in place, snapshots the database, and keeps the data"

# -------------------------------------------------------------------- rollback

info "Upgrade where the new build fails to start"
if run_installer STUB_FAIL_START=1 -- > "$SANDBOX/rollback.log" 2>&1; then
  cat "$SANDBOX/rollback.log"
  fail "a failed upgrade should exit non-zero"
fi
grep -q 'Rolling back' "$SANDBOX/rollback.log" || {
  cat "$SANDBOX/rollback.log"
  fail "no rollback was attempted"
}
[ ! -e "$SANDBOX/opt/deployer.prev" ] || fail "rollback left deployer.prev in place"
[ -x "$SANDBOX/opt/deployer" ] || fail "rollback did not restore a runnable binary"
pass "a failed upgrade rolls back to the previous version"

# The rollback restarts the old binary through the stub, which was told not to
# start anything — so bring it back up the way the stub would have.
STUB_DIR="$SANDBOX/stub" DEPLOYER_UNIT="$SANDBOX/deployer.service" systemctl start deployer.service
for _ in $(seq 1 20); do
  if curl -fsS --max-time 2 "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1; then break; fi
  sleep 0.5
done
curl -fsS --max-time 3 "http://127.0.0.1:$PORT/api/hosts" | grep -q upgrade-canary ||
  fail "the rolled-back version lost the data"
pass "the rolled-back version still has its data"

# ------------------------------------------------------------------- uninstall

info "Uninstall"
run_installer -- --uninstall > "$SANDBOX/uninstall.log" 2>&1 ||
  fail "uninstall exited non-zero"
[ ! -e "$SANDBOX/opt" ] || fail "uninstall left the install directory"
[ ! -e "$SANDBOX/deployer.service" ] || fail "uninstall left the unit file"
[ -f "$SANDBOX/data/deployer.db" ] || fail "uninstall deleted the database — it must be kept"
pass "uninstall removes the service but keeps the data"

echo
printf '\033[1;32mAll installer checks passed.\033[0m\n'
