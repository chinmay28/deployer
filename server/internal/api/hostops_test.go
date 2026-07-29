package api

import (
	"fmt"
	"net/http"
	"testing"
)

// These cover the answers the API gives on its own — the ones that come back
// before anything connects to a host. What happens on the host is the hostops
// package's business, and is tested there against a real filesystem.

// newHost creates a host row to hang requests off.
func newHost(t *testing.T, h http.Handler) int64 {
	t.Helper()
	w := do(t, h, "POST", "/api/hosts", `{"name":"pi","address":"nakedpi.local","username":"chinmay"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create host: %d %s", w.Code, w.Body)
	}
	return decode[hostView](t, w).ID
}

func TestFileRequestsValidateThePath(t *testing.T) {
	_, h := testServer(t, "")
	id := newHost(t, h)

	cases := []struct{ name, method, path, body string }{
		{"read without a path", "GET", "/api/hosts/%d/files/content", ""},
		{"read a relative path", "GET", "/api/hosts/%d/files/content?path=etc/hosts", ""},
		{"read a home-relative path", "GET", "/api/hosts/%d/files/content?path=~/.bashrc", ""},
		{"delete without a path", "DELETE", "/api/hosts/%d/files", ""},
		{"write without a path", "PUT", "/api/hosts/%d/files/content", `{"path":"","content":"x"}`},
		{"write a relative path", "PUT", "/api/hosts/%d/files/content", `{"path":"etc/hosts","content":"x"}`},
		{"mkdir with a relative path", "POST", "/api/hosts/%d/files/mkdir", `{"path":"tmp/x"}`},
		{"rename to a relative path", "POST", "/api/hosts/%d/files/rename", `{"path":"/tmp/a","to":"b"}`},
		{"rename from nothing", "POST", "/api/hosts/%d/files/rename", `{"path":"","to":"/tmp/b"}`},
		{"an unknown field", "POST", "/api/hosts/%d/files/mkdir", `{"path":"/tmp/x","mode":"755"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, h, tc.method, fmt.Sprintf(tc.path, id), tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body)
			}
			if decode[apiError](t, w).Error == "" {
				t.Error("a 400 should say what was wrong with the request")
			}
		})
	}
}

func TestPowerValidatesTheAction(t *testing.T) {
	_, h := testServer(t, "")
	id := newHost(t, h)

	for _, body := range []string{`{"action":"halt"}`, `{"action":""}`, `{}`, `{"action":"reboot; ls"}`} {
		w := do(t, h, "POST", fmt.Sprintf("/api/hosts/%d/power", id), body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("power %s = %d, want 400 (body %s)", body, w.Code, w.Body)
		}
	}
}

// A host that does not exist is a 404 whatever is asked of it, and is answered
// without dialling anything.
func TestHostOperationsOnAMissingHost(t *testing.T) {
	_, h := testServer(t, "")
	cases := []struct{ method, path, body string }{
		{"POST", "/api/hosts/999/power", `{"action":"reboot"}`},
		{"GET", "/api/hosts/999/cron", ""},
		{"PUT", "/api/hosts/999/cron", `{"user":"","content":""}`},
		{"GET", "/api/hosts/999/files?path=/etc", ""},
		{"GET", "/api/hosts/999/files/content?path=/etc/hosts", ""},
		{"PUT", "/api/hosts/999/files/content", `{"path":"/tmp/x","content":"x"}`},
		{"POST", "/api/hosts/999/files/mkdir", `{"path":"/tmp/x"}`},
		{"POST", "/api/hosts/999/files/rename", `{"path":"/tmp/a","to":"/tmp/b"}`},
		{"DELETE", "/api/hosts/999/files?path=/tmp/x", ""},
	}
	for _, tc := range cases {
		w := do(t, h, tc.method, tc.path, tc.body)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404 (body %s)", tc.method, tc.path, w.Code, w.Body)
		}
	}
}

// Managing a host is behind the PIN like everything else.
func TestHostOperationsNeedTheSession(t *testing.T) {
	_, h := testServer(t, "1234")
	for _, path := range []string{
		"/api/hosts/1/files?path=/etc",
		"/api/hosts/1/files/content?path=/etc/hosts",
		"/api/hosts/1/cron",
	} {
		if w := do(t, h, "GET", path, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a session = %d, want 401", path, w.Code)
		}
	}
	if w := do(t, h, "POST", "/api/hosts/1/power", `{"action":"reboot"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("power without a session = %d, want 401", w.Code)
	}
}
