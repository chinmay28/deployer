package hostops

import (
	"context"
	"errors"
	"fmt"

	"github.com/chinmay28/deployer/server/internal/store"
)

// Power actions.
const (
	ActionReboot   = "reboot"
	ActionShutdown = "shutdown"
)

// powerScript schedules the restart in a detached process and returns
// immediately, so Deployer gets a clean answer instead of the connection dying
// mid-command and having to guess what that meant. The few seconds' delay is
// what buys that: sshd hangs up the remote command when its client goes away,
// so the command that takes the machine down must outlive the session it was
// asked from — the same reason a self-update runs detached.
//
// systemd first, then the SysV commands, so this works on a host without it.
const powerScript = `set -u
a=$1
[ "$(id -u)" = 0 ] || { printf 'this needs root\n' >&2; exit 2; }
case "$a" in
  reboot) c='systemctl reboot || shutdown -r now || reboot';;
  shutdown) c='systemctl poweroff || shutdown -h now || poweroff';;
  *) printf 'unknown action: %s\n' "$a" >&2; exit 3;;
esac
if command -v setsid >/dev/null 2>&1; then
  nohup setsid sh -c "sleep 3; $c" >/dev/null 2>&1 </dev/null &
else
  nohup sh -c "sleep 3; $c" >/dev/null 2>&1 </dev/null &
fi
printf 'scheduled\n'
`

// Power restarts or shuts down a host. It returns once the host has accepted
// the request, which is a few seconds before the machine actually goes down.
func (s *Service) Power(ctx context.Context, h *store.Host, action string) error {
	if action != ActionReboot && action != ActionShutdown {
		return invalid("unknown power action %q", action)
	}
	res, err := s.run(ctx, h, elevate(powerScript, action), "")
	if err != nil {
		return err
	}
	if res.ExitCode == 2 {
		return fmt.Errorf("%w: %s needs passwordless sudo on %s to do that", ErrNeedsRoot, h.Username, h.Name)
	}
	if res.ExitCode != 0 {
		return failure(res, "could not "+action+" "+h.Name)
	}
	return nil
}

// ErrNeedsRoot means the operation was refused for want of privileges, rather
// than having failed on its own terms.
var ErrNeedsRoot = errors.New("this needs root on the host")
