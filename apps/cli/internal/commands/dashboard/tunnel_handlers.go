package dashboard

import (
	"net/http"
	"strings"
	"time"

	appdashboard "github.com/cofy-x/axern/apps/cli/internal/application/dashboard"
	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
)

func (s *server) handleTunnels(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path != "/api/tunnels" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	resp, err := s.dashboard.Tunnels(r.Context(), appdashboard.TunnelListParams{
		AllocationID:    r.URL.Query().Get("allocation_id"),
		NodeID:          r.URL.Query().Get("node_id"),
		IncludeTerminal: parseBool(r.URL.Query().Get("include_terminal")),
	})
	writeResult(w, map[string]any{"tunnels": resp}, err)
}

func (s *server) handleTunnelPath(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/tunnels/"), "/")
	sessionID := strings.TrimSpace(parts[0])
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id is required")
		return
	}
	switch {
	case len(parts) == 1:
		resp, err := s.dashboard.TunnelDetail(r.Context(), sessionID)
		writeResult(w, resp, err)
	case len(parts) == 2 && parts[1] == "events":
		limit, ok := parseLimit(w, r)
		if !ok {
			return
		}
		resp, err := s.dashboard.TunnelEvents(r.Context(), sessionID, limit)
		writeResult(w, map[string]any{"events": resp}, err)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *server) handleTunnelDoctor(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	allocationID := strings.TrimSpace(r.URL.Query().Get("allocation_id"))
	serviceID := strings.TrimSpace(r.URL.Query().Get("service_id"))
	selected := 0
	for _, value := range []string{sessionID, allocationID, serviceID} {
		if value != "" {
			selected++
		}
	}
	if selected != 1 {
		writeError(w, http.StatusBadRequest, "exactly one of session_id, allocation_id, or service_id is required")
		return
	}
	resp, err := s.dashboard.TunnelDoctorWithService(r.Context(), apptunnel.DoctorParams{
		SessionID:    sessionID,
		AllocationID: allocationID,
		ServiceID:    serviceID,
		Timeout:      5 * time.Second,
	}, s.serviceClient)
	writeResult(w, resp, err)
}
