package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/chinmay28/deployer/server/internal/hostops"
	"github.com/chinmay28/deployer/server/internal/store"
)

// Managing a host — its files, its crontab, its power state — is work Deployer
// asks the host to do. So the failures worth telling apart are "you asked for
// something impossible" (400) and "the host refused" (502): a missing file or a
// read-only filesystem is not a bug in this API, and saying so as a 500 would
// send the user looking in the wrong place.

// opTimeout bounds one operation on a host. Generous enough for a slow
// handshake to a sleeping Pi, short enough that a hung host frees the request.
const opTimeout = 30 * time.Second

// opContext gives a host operation its own deadline.
func opContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), opTimeout)
}

// writeOpError maps a failure on the host onto a status code.
func (s *Server) writeOpError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, hostops.ErrInvalid):
		// Deployer refused it, not the host: the request is what needs fixing.
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, hostops.ErrNotEmpty):
		// The caller can retry asking for the contents to go too.
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, hostops.ErrNeedsRoot):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		s.Log.Debug("api: "+action, "err", err)
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// --- power ---

type powerInput struct {
	Action string `json:"action"`
}

// handleHostPower reboots or shuts a host down. The host is marked offline
// straight away: it is about to go, and a UI that keeps calling it online until
// the next poll fails would be lying for half a minute.
func (s *Server) handleHostPower(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "power")
		return
	}
	var in powerInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Action != hostops.ActionReboot && in.Action != hostops.ActionShutdown {
		writeError(w, http.StatusBadRequest, `action must be "reboot" or "shutdown"`)
		return
	}

	ctx, cancel := opContext(r)
	defer cancel()
	if err := s.Ops.Power(ctx, h, in.Action); err != nil {
		s.writeOpError(w, err, "power host")
		return
	}
	s.Log.Info("api: host power", "host", h.Name, "action", in.Action)

	reason := "rebooting — waiting for it to come back"
	if in.Action == hostops.ActionShutdown {
		reason = "shutting down"
	}
	if err := s.DB.MarkHostFailed(r.Context(), h.ID, store.StatusOffline, reason); err != nil {
		s.Log.Error("api: mark host going down", "host", h.Name, "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"action": in.Action, "status": reason})
}

// --- crontab ---

func (s *Server) handleGetCrontab(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "read crontab")
		return
	}
	ctx, cancel := opContext(r)
	defer cancel()

	cron, err := s.Ops.ReadCrontab(ctx, h, r.URL.Query().Get("user"))
	if err != nil {
		s.writeOpError(w, err, "read crontab")
		return
	}
	writeJSON(w, http.StatusOK, cron)
}

type crontabInput struct {
	User    string `json:"user"`
	Content string `json:"content"`
}

// handlePutCrontab installs a crontab. cron parses it on the way in and keeps
// the old one if it cannot, so a syntax error comes back as the host's own
// complaint rather than as a broken schedule.
func (s *Server) handlePutCrontab(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "write crontab")
		return
	}
	var in crontabInput
	if err := decodeJSONLimit(r, &in, 2*hostops.MaxCrontabBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := opContext(r)
	defer cancel()

	if err := s.Ops.WriteCrontab(ctx, h, in.User, in.Content); err != nil {
		s.writeOpError(w, err, "write crontab")
		return
	}
	s.Log.Info("api: wrote crontab", "host", h.Name, "user", in.User)

	cron, err := s.Ops.ReadCrontab(ctx, h, in.User)
	if err != nil {
		// It was installed; failing to read it back is not worth an error.
		writeJSON(w, http.StatusOK, &hostops.Crontab{User: in.User, Content: in.Content, Exists: true})
		return
	}
	writeJSON(w, http.StatusOK, cron)
}

// --- files ---

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "list files")
		return
	}
	ctx, cancel := opContext(r)
	defer cancel()

	// An absent path means the SSH user's home, which only the host knows.
	listing, err := s.Ops.List(ctx, h, r.URL.Query().Get("path"))
	if err != nil {
		s.writeOpError(w, err, "list files")
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "read file")
		return
	}
	path, ok := queryPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := opContext(r)
	defer cancel()

	file, err := s.Ops.Read(ctx, h, path)
	if err != nil {
		s.writeOpError(w, err, "read file")
		return
	}
	writeJSON(w, http.StatusOK, file)
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "write file")
		return
	}
	var in writeFileInput
	// Room for the file itself, plus what JSON escaping may add to it.
	if err := decodeJSONLimit(r, &in, 4*hostops.MaxWriteBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := hostops.CleanPath(in.Path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := opContext(r)
	defer cancel()

	if err := s.Ops.Write(ctx, h, in.Path, in.Content); err != nil {
		s.writeOpError(w, err, "write file")
		return
	}
	s.Log.Info("api: wrote file", "host", h.Name, "path", in.Path, "bytes", len(in.Content))

	// Answer with the file as it now is, so the editor picks up the size and
	// modification time it was just given rather than guessing at them.
	file, err := s.Ops.Read(ctx, h, in.Path)
	if err != nil {
		writeJSON(w, http.StatusOK, &hostops.File{Path: in.Path, Content: in.Content})
		return
	}
	writeJSON(w, http.StatusOK, file)
}

type pathInput struct {
	Path string `json:"path"`
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "create directory")
		return
	}
	var in pathInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	clean, err := hostops.CleanPath(in.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := opContext(r)
	defer cancel()

	if err := s.Ops.Mkdir(ctx, h, clean); err != nil {
		s.writeOpError(w, err, "create directory")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": clean})
}

type renameInput struct {
	Path string `json:"path"`
	To   string `json:"to"`
}

func (s *Server) handleRenameFile(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "rename")
		return
	}
	var in renameInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, err := hostops.CleanPath(in.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := hostops.CleanPath(in.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := opContext(r)
	defer cancel()

	if err := s.Ops.Rename(ctx, h, from, to); err != nil {
		s.writeOpError(w, err, "rename")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": to})
}

func (s *Server) handleRemoveFile(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "delete file")
		return
	}
	path, ok := queryPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := opContext(r)
	defer cancel()

	recursive := r.URL.Query().Get("recursive") == "true"
	if err := s.Ops.Remove(ctx, h, path, recursive); err != nil {
		s.writeOpError(w, err, "delete file")
		return
	}
	s.Log.Info("api: deleted", "host", h.Name, "path", path, "recursive", recursive)
	w.WriteHeader(http.StatusNoContent)
}

// queryPath reads and validates the ?path= parameter, answering the request
// itself when it is missing or malformed.
func queryPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	clean, err := hostops.CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return clean, true
}
