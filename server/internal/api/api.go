// Package api exposes Deployer's REST API over HTTP.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/chinmay28/deployer/server/internal/hosts"
	"github.com/chinmay28/deployer/server/internal/store"
)

// Server holds the dependencies the handlers need.
type Server struct {
	DB     *store.DB
	Hosts  *hosts.Service
	Poller *hosts.Poller
	Log    *slog.Logger
	// Auth is nil when Deployer runs without a PIN.
	Auth *PinAuth
}

// Routes returns the API mux, to be mounted under /api/.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)

	mux.HandleFunc("GET /api/session", s.handleSessionStatus)
	mux.HandleFunc("POST /api/session", s.handleLogin)

	mux.HandleFunc("GET /api/settings/ssh", s.handleGetSSHKey)
	mux.HandleFunc("POST /api/settings/ssh/rotate", s.handleRotateSSHKey)

	mux.HandleFunc("GET /api/hosts", s.handleListHosts)
	mux.HandleFunc("POST /api/hosts", s.handleCreateHost)
	mux.HandleFunc("GET /api/hosts/{id}", s.handleGetHost)
	mux.HandleFunc("PATCH /api/hosts/{id}", s.handleUpdateHost)
	mux.HandleFunc("DELETE /api/hosts/{id}", s.handleDeleteHost)
	mux.HandleFunc("POST /api/hosts/{id}/test", s.handleTestHost)
	mux.HandleFunc("GET /api/hosts/{id}/metrics", s.handleHostMetrics)

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent; nothing useful left to do.
		return
	}
}

type apiError struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, apiError{Error: msg})
}

// writeStoreError maps a store error onto a status code.
func (s *Server) writeStoreError(w http.ResponseWriter, err error, action string) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a host with that name already exists")
		return
	}
	s.Log.Error("api: "+action, "err", err)
	writeError(w, http.StatusInternalServerError, action+" failed")
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}
