package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chinmay28/deployer/server/internal/deploy"
	"github.com/chinmay28/deployer/server/internal/hosts"
	"github.com/chinmay28/deployer/server/internal/sshx"
	"github.com/chinmay28/deployer/server/internal/store"
)

func testServer(t *testing.T, pin string) (*Server, http.Handler) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "deployer.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	id, err := sshx.EnsureIdentity(context.Background(), db)
	if err != nil {
		t.Fatalf("ensure identity: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := hosts.NewService(db, id, nil)
	health := deploy.NewChecker(db, svc, log)
	s := &Server{
		DB:     db,
		Hosts:  svc,
		Poller: hosts.NewPoller(svc, db, log),
		Runner: deploy.NewRunner(db, svc, health, log),
		Health: health,
		Log:    log,
		Auth:   NewPinAuth(pin),
	}
	return s, s.Auth.Middleware(s.Routes())
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return v
}

func TestHealth(t *testing.T) {
	_, h := testServer(t, "")
	w := do(t, h, "GET", "/api/health", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
}

func TestCreateHostValidation(t *testing.T) {
	_, h := testServer(t, "")
	cases := []struct {
		name, body, wantMsg string
	}{
		{"no name", `{"address":"pi.local","username":"u"}`, "name is required"},
		{"no address", `{"name":"pi","username":"u"}`, "address is required"},
		{"no username", `{"name":"pi","address":"pi.local"}`, "username is required"},
		{"bad port", `{"name":"pi","address":"pi.local","username":"u","port":70000}`, "port must be"},
		{"unknown field", `{"name":"pi","nope":1}`, "invalid request body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, h, "POST", "/api/hosts", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body)
			}
			if got := decode[apiError](t, w).Error; !strings.Contains(got, tc.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", got, tc.wantMsg)
			}
		})
	}
}

func TestHostLifecycle(t *testing.T) {
	_, h := testServer(t, "")

	// Port defaults to 22 when omitted.
	w := do(t, h, "POST", "/api/hosts", `{"name":" pi ","address":"nakedpi.local","username":"chinmay"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", w.Code, w.Body)
	}
	created := decode[hostView](t, w)
	if created.Port != 22 {
		t.Errorf("port = %d, want default 22", created.Port)
	}
	if created.Name != "pi" {
		t.Errorf("name = %q, want it trimmed to \"pi\"", created.Name)
	}
	if created.Latest != nil {
		t.Errorf("latest = %+v, want null before any sample", created.Latest)
	}

	// The private key must never leave the server.
	if strings.Contains(w.Body.String(), "PRIVATE KEY") {
		t.Fatal("host response leaked key material")
	}

	if w := do(t, h, "POST", "/api/hosts", `{"name":"pi","address":"other","username":"u"}`); w.Code != http.StatusConflict {
		t.Errorf("duplicate name status = %d, want 409", w.Code)
	}

	list := decode[[]hostView](t, do(t, h, "GET", "/api/hosts", ""))
	if len(list) != 1 {
		t.Fatalf("list returned %d hosts, want 1", len(list))
	}

	w = do(t, h, "PATCH", "/api/hosts/1", `{"address":"192.168.2.123","port":2222}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body %s", w.Code, w.Body)
	}
	patched := decode[hostView](t, w)
	if patched.Address != "192.168.2.123" || patched.Port != 2222 {
		t.Errorf("patched = %+v", patched.Host)
	}
	// Fields left out of the PATCH body keep their previous values.
	if patched.Name != "pi" || patched.Username != "chinmay" {
		t.Errorf("patch clobbered untouched fields: %+v", patched.Host)
	}

	if w := do(t, h, "GET", "/api/hosts/404", ""); w.Code != http.StatusNotFound {
		t.Errorf("get missing status = %d, want 404", w.Code)
	}
	if w := do(t, h, "DELETE", "/api/hosts/1", ""); w.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", w.Code)
	}
	if w := do(t, h, "DELETE", "/api/hosts/1", ""); w.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", w.Code)
	}
}

func TestHostMetricsEmpty(t *testing.T) {
	_, h := testServer(t, "")
	do(t, h, "POST", "/api/hosts", `{"name":"pi","address":"pi.local","username":"u"}`)

	w := do(t, h, "GET", "/api/hosts/1/metrics?minutes=15", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	got := decode[struct {
		Minutes int             `json:"minutes"`
		Samples []*store.Sample `json:"samples"`
	}](t, w)
	if got.Minutes != 15 {
		t.Errorf("minutes = %d, want 15", got.Minutes)
	}
	if got.Samples == nil {
		t.Error("samples = null, want an empty array so the UI can map over it")
	}
}

func TestSSHKeyEndpoint(t *testing.T) {
	_, h := testServer(t, "")
	w := do(t, h, "GET", "/api/settings/ssh", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	first := decode[sshKeyView](t, w)
	if !strings.HasPrefix(first.PublicKey, "ssh-ed25519 ") {
		t.Errorf("publicKey = %q", first.PublicKey)
	}
	if !strings.HasPrefix(first.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint = %q", first.Fingerprint)
	}
	if !strings.Contains(first.AuthorizeCommand, first.PublicKey) {
		t.Error("authorize command should embed the public key")
	}

	// The key is stable across calls...
	if again := decode[sshKeyView](t, do(t, h, "GET", "/api/settings/ssh", "")); again.PublicKey != first.PublicKey {
		t.Error("public key changed between reads")
	}
	// ...until it is rotated.
	rotated := decode[sshKeyView](t, do(t, h, "POST", "/api/settings/ssh/rotate", ""))
	if rotated.PublicKey == first.PublicKey {
		t.Error("rotate returned the same key")
	}
	if now := decode[sshKeyView](t, do(t, h, "GET", "/api/settings/ssh", "")); now.PublicKey != rotated.PublicKey {
		t.Error("rotated key was not persisted")
	}
}

func TestPinAuth(t *testing.T) {
	_, h := testServer(t, "1234")

	// Health and session status stay open so the UI can bootstrap.
	if w := do(t, h, "GET", "/api/health", ""); w.Code != http.StatusOK {
		t.Errorf("health status = %d, want 200 without a session", w.Code)
	}
	status := decode[map[string]bool](t, do(t, h, "GET", "/api/session", ""))
	if !status["required"] || status["authenticated"] {
		t.Errorf("session status = %v, want required and unauthenticated", status)
	}

	if w := do(t, h, "GET", "/api/hosts", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("hosts status = %d, want 401", w.Code)
	}
	if w := do(t, h, "POST", "/api/session", `{"pin":"wrong"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad pin status = %d, want 401", w.Code)
	}

	w := do(t, h, "POST", "/api/session", `{"pin":"1234"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, body %s", w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login set no cookie")
	}
	if !cookies[0].HttpOnly {
		t.Error("session cookie should be HttpOnly")
	}

	r := httptest.NewRequest("GET", "/api/hosts", nil)
	r.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("authenticated hosts status = %d, want 200", rec.Code)
	}
}

func TestNoAuthByDefault(t *testing.T) {
	_, h := testServer(t, "")
	if w := do(t, h, "GET", "/api/hosts", ""); w.Code != http.StatusOK {
		t.Fatalf("hosts status = %d, want 200 when no PIN is configured", w.Code)
	}
	status := decode[map[string]bool](t, do(t, h, "GET", "/api/session", ""))
	if status["required"] || !status["authenticated"] {
		t.Errorf("session status = %v, want not required and authenticated", status)
	}
}
