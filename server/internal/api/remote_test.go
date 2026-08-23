package api

import (
	"fmt"
	"net/http"
	"testing"
)

// A remote session runs a browser on the host and hands it a page to open, so
// the values that reach it are worth refusing here — before anything connects,
// and long before anything reaches a command line on somebody's Raspberry Pi.
func TestRemoteSessionRequestsAreValidatedBeforeConnecting(t *testing.T) {
	_, h := testServer(t, "")
	id := newHost(t, h)

	cases := []struct{ name, method, path, body string }{
		{"a screen size that is not one", "POST", "/api/hosts/%d/remote", `{"geometry":"huge"}`},
		{"a screen too small to lay a site out", "POST", "/api/hosts/%d/remote", `{"geometry":"320x240"}`},
		{"a screen size carrying a command", "POST", "/api/hosts/%d/remote", `{"geometry":"1280x800;reboot"}`},
		{"a privileged port", "POST", "/api/hosts/%d/remote", `{"port":80}`},
		{"the session's own VNC port", "POST", "/api/hosts/%d/remote", `{"port":5999}`},
		{"a page that is not a web address", "POST", "/api/hosts/%d/remote", `{"homepage":"example.com"}`},
		{"a page that is a script", "POST", "/api/hosts/%d/remote", `{"homepage":"javascript:alert(1)"}`},
		{"a page that is a local file", "POST", "/api/hosts/%d/remote", `{"homepage":"file:///etc/shadow"}`},
		{"a page that is a browser option", "POST", "/api/hosts/%d/remote", `{"homepage":"--headless"}`},
		{"an unknown field", "POST", "/api/hosts/%d/remote", `{"geometry":"1280x800","display":9}`},
		{"an action systemd was not asked for", "POST", "/api/hosts/%d/remote/action", `{"action":"pause"}`},
		{"no action at all", "POST", "/api/hosts/%d/remote/action", `{"url":"https://example.com/"}`},
		{"a start with a page that is a script", "POST", "/api/hosts/%d/remote/action", `{"action":"start","url":"javascript:alert(1)"}`},
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

// A host that does not exist is a 404 whatever is asked of its session, rather
// than an attempt to connect to nothing.
func TestRemoteSessionRequestsNeedAHost(t *testing.T) {
	_, h := testServer(t, "")
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/hosts/999/remote", ""},
		{"POST", "/api/hosts/999/remote", `{}`},
		{"POST", "/api/hosts/999/remote/action", `{"action":"stop"}`},
		{"DELETE", "/api/hosts/999/remote", ""},
	} {
		w := do(t, h, tc.method, tc.path, tc.body)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404 (body %s)", tc.method, tc.path, w.Code, w.Body)
		}
	}
}
