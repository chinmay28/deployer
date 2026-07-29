package hostops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chinmay28/deployer/server/internal/store"
)

// Only the branches that refuse are exercised here. The branch that succeeds
// ends in `systemctl reboot`, and the machine running the tests is as capable
// of obeying that as any other.

func TestPowerScriptRefusesAnUnknownAction(t *testing.T) {
	for _, action := range []string{"halt", "", "reboot; rm -rf /", "REBOOT"} {
		out, code := runScript(t, asUser(powerScript, action), "")
		if code != 3 {
			t.Errorf("%q exited %d (%q), want 3 — nothing should be scheduled", action, code, out)
		}
	}
}

// Without root there is nothing to do but say so, rather than scheduling a
// command that will fail silently three seconds after the reply was sent.
func TestPowerScriptNeedsRoot(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "id"), []byte("#!/bin/sh\necho 1000\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, code := runScript(t, asUser(powerScript, ActionReboot), "", bin)
	if code != 2 {
		t.Fatalf("exit = %d (%q), want 2", code, out)
	}
}

// The action is checked before anything connects, so a typo cannot become a
// command on a host.
func TestPowerRejectsUnknownActionBeforeConnecting(t *testing.T) {
	svc := NewService(nil) // any connection attempt would panic
	err := svc.Power(context.Background(), &store.Host{Name: "pi"}, "halt")
	if err == nil || !strings.Contains(err.Error(), "halt") {
		t.Fatalf("err = %v, want it to name the action it refused", err)
	}
}
