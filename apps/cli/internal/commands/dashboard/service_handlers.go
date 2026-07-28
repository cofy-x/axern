package dashboard

import (
	"net/http"
	"net/url"
	"strings"

	appdashboard "github.com/cofy-x/axern/apps/cli/internal/application/dashboard"
)

func (s *server) handleSummary(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	resp, err := s.dashboard.Summary(r.Context())
	writeResult(w, resp, err)
}

func (s *server) handleReconcileHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	resp, err := s.dashboard.ReconcileHealth(r.Context())
	writeResult(w, resp, err)
}

func (s *server) handleServices(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path != "/api/services" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	resp, err := s.dashboard.Services(r.Context())
	writeResult(w, map[string]any{"services": resp}, err)
}

func (s *server) handleQuotas(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path != "/api/quotas" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	resp, err := s.dashboard.Quotas(r.Context())
	writeResult(w, map[string]any{"quotas": resp}, err)
}

func (s *server) handleQuotaPath(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/quotas/"), "/")
	namespace, err := url.PathUnescape(strings.TrimSpace(parts[0]))
	if err != nil || strings.TrimSpace(namespace) == "" {
		writeError(w, http.StatusBadRequest, "namespace is required")
		return
	}
	if len(parts) != 2 || parts[1] != "events" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	resp, err := s.dashboard.QuotaEvents(r.Context(), namespace, limit)
	writeResult(w, map[string]any{"events": resp}, err)
}

func (s *server) handleServicePath(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/services/"), "/")
	serviceID := strings.TrimSpace(parts[0])
	if serviceID == "" {
		writeError(w, http.StatusBadRequest, "service id is required")
		return
	}
	switch {
	case len(parts) == 1:
		resp, err := s.dashboard.ServiceDetail(r.Context(), serviceID)
		writeResult(w, resp, err)
	case len(parts) == 2 && parts[1] == "replicas":
		view, err := appdashboard.ParseServiceReplicaView(r.URL.Query().Get("view"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp, err := s.dashboard.ServiceReplicas(r.Context(), serviceID, view)
		writeResult(w, map[string]any{"replicas": resp}, err)
	case len(parts) == 2 && parts[1] == "events":
		limit, ok := parseLimit(w, r)
		if !ok {
			return
		}
		resp, err := s.dashboard.ServiceEvents(r.Context(), serviceID, limit)
		writeResult(w, map[string]any{"events": resp}, err)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}
