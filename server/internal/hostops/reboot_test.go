package hostops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Only the branch that refuses is exercised here. The branch that succeeds ends
// in `systemctl reboot`, and the machine running the tests is as capable of
// obeying that as any other.

// Without root there is nothing to do but say so, rather than scheduling a
// command that will fail silently three seconds after the reply was sent.
func TestRebootScriptNeedsRoot(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "id"), []byte("#!/bin/sh\necho 1000\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, code := runScript(t, asUser(rebootScript), "", bin)
	if code != 2 {
		t.Fatalf("exit = %d (%q), want 2", code, out)
	}
}

// Shutting a host down is not something this package can be asked to do: there
// is no way back from it, so there is no code for it.
func TestThereIsNoWayToShutAHostDown(t *testing.T) {
	for _, script := range []string{rebootScript, listScript, writeScript, removeScript} {
		for _, word := range []string{"poweroff", "shutdown -h", "halt"} {
			if strings.Contains(script, word) {
				t.Errorf("a script mentions %q; nothing here should be able to power a host off", word)
			}
		}
	}
}
