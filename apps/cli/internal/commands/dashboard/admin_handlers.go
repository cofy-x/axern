package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"

	appadmin "github.com/cofy-x/axern/apps/cli/internal/application/admin"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path != "/api/admin" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	resp, err := s.dashboard.Admin(r.Context())
	writeResult(w, resp, err)
}

type adminRetryActionRequest struct {
	Reason         string `json:"reason"`
	OperatorReason string `json:"operator_reason"`
}

func (s *server) handleAdminAllocationRetryPath(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/admin/allocation-retries/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	allocationID := strings.TrimSpace(parts[0])
	action := strings.TrimSpace(parts[1])
	if allocationID == "" {
		writeError(w, http.StatusBadRequest, "allocation id is required")
		return
	}
	var req adminRetryActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if err := appadmin.ValidateOperatorReason(req.OperatorReason); err != nil {
		writeAdminError(w, grpcstatus.Error(codes.InvalidArgument, err.Error()))
		return
	}
	switch action {
	case "force":
		if err := appadmin.ValidateRetryReason(req.Reason); err != nil {
			writeAdminError(w, grpcstatus.Error(codes.InvalidArgument, err.Error()))
			return
		}
		resp, err := s.dashboard.ForceAllocationLifecycleRetry(r.Context(), allocationID, req.Reason, req.OperatorReason)
		writeAdminActionResult(w, resp, err)
	case "fail":
		if strings.TrimSpace(req.Reason) != "" && appadmin.ParseRetryReason(req.Reason) != appadmin.ParseRetryReason("create") {
			writeAdminError(w, grpcstatus.Error(codes.InvalidArgument, "fail only supports create retry reason"))
			return
		}
		resp, err := s.dashboard.FailAllocationLifecycleCreateRetry(r.Context(), allocationID, req.OperatorReason)
		writeAdminActionResult(w, resp, err)
	case "clear":
		if err := appadmin.ValidateRetryReason(req.Reason); err != nil {
			writeAdminError(w, grpcstatus.Error(codes.InvalidArgument, err.Error()))
			return
		}
		resp, err := s.dashboard.ClearAllocationLifecycleRetry(r.Context(), allocationID, req.Reason, req.OperatorReason)
		writeAdminActionResult(w, resp, err)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func writeAdminActionResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeResult(w, value, nil)
}
