package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chinmay28/deployer/server/internal/store"
)

// hostView is a host plus its most recent telemetry.
type hostView struct {
	*store.Host
	Latest *store.Sample `json:"latest"`
}

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.ListHosts(r.Context())
	if err != nil {
		s.writeStoreError(w, err, "list hosts")
		return
	}
	latest, err := s.DB.LatestSamples(r.Context())
	if err != nil {
		s.writeStoreError(w, err, "list hosts")
		return
	}
	views := make([]hostView, 0, len(list))
	for _, h := range list {
		views = append(views, hostView{Host: h, Latest: latest[h.ID]})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) handleGetHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "get host")
		return
	}
	latest, err := s.DB.LatestSamples(r.Context())
	if err != nil {
		s.writeStoreError(w, err, "get host")
		return
	}
	writeJSON(w, http.StatusOK, hostView{Host: h, Latest: latest[h.ID]})
}

// hostInput is the editable shape of a host.
type hostInput struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Username string `json:"username"`
}

// normalize trims input and applies defaults, returning a validation message.
func (in *hostInput) normalize() string {
	in.Name = strings.TrimSpace(in.Name)
	in.Address = strings.TrimSpace(in.Address)
	in.Username = strings.TrimSpace(in.Username)
	if in.Port == 0 {
		in.Port = 22
	}
	switch {
	case in.Name == "":
		return "name is required"
	case in.Address == "":
		return "address is required (hostname like nakedpi.local, or an IP)"
	case in.Username == "":
		return "username is required"
	case in.Port < 1 || in.Port > 65535:
		return "port must be between 1 and 65535"
	}
	return ""
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var in hostInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if msg := in.normalize(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	h, err := s.DB.CreateHost(r.Context(), &store.Host{
		Name: in.Name, Address: in.Address, Port: in.Port, Username: in.Username,
	})
	if err != nil {
		s.writeStoreError(w, err, "create host")
		return
	}
	// Check reachability right away so the UI can show the result immediately.
	go s.probeInBackground(h)
	writeJSON(w, http.StatusCreated, hostView{Host: h})
}

func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "update host")
		return
	}
	in := hostInput{Name: h.Name, Address: h.Address, Port: h.Port, Username: h.Username}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if msg := in.normalize(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	h.Name, h.Address, h.Port, h.Username = in.Name, in.Address, in.Port, in.Username
	if err := s.DB.UpdateHost(r.Context(), h); err != nil {
		s.writeStoreError(w, err, "update host")
		return
	}
	writeJSON(w, http.StatusOK, hostView{Host: h})
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid host id")
		return
	}
	if err := s.DB.DeleteHost(r.Context(), id); err != nil {
		s.writeStoreError(w, err, "delete host")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "test host")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.Hosts.Test(ctx, h))
}

func (s *Server) handleHostMetrics(w http.ResponseWriter, r *http.Request) {
	h, err := s.hostFromPath(r)
	if err != nil {
		s.writeStoreError(w, err, "host metrics")
		return
	}
	// Someone is looking at this host: poll it more often for a while.
	s.Poller.Watch(h.ID)

	minutes := 60
	if v := r.URL.Query().Get("minutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 24*60 {
			minutes = n
		}
	}
	samples, err := s.DB.SamplesSince(r.Context(), h.ID, time.Now().Add(-time.Duration(minutes)*time.Minute))
	if err != nil {
		s.writeStoreError(w, err, "host metrics")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hostId":  h.ID,
		"minutes": minutes,
		"samples": samples,
	})
}

func (s *Server) hostFromPath(r *http.Request) (*store.Host, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, store.ErrNotFound
	}
	return s.DB.GetHost(r.Context(), id)
}

// probeInBackground samples a host outside the request lifetime.
func (s *Server) probeInBackground(h *store.Host) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.Hosts.Probe(ctx, h); err != nil {
		s.Log.Debug("api: initial probe failed", "host", h.Name, "err", err)
	}
}
