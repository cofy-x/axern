package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	appdashboard "github.com/cofy-x/axern/apps/cli/internal/application/dashboard"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func parseLimit(w http.ResponseWriter, r *http.Request) (int32, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return appdashboard.DefaultEventLimit, true
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "limit must be a non-negative integer")
		return 0, false
	}
	return appdashboard.NormalizeLimit(int32(n)), true
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func writeResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func writeAdminError(w http.ResponseWriter, err error) {
	code := grpcstatus.Code(err)
	status := http.StatusBadGateway
	class := "unknown"
	switch code {
	case codes.NotFound:
		status = http.StatusNotFound
		class = "not-found"
	case codes.FailedPrecondition:
		status = http.StatusConflict
		class = "failed-precondition"
	case codes.InvalidArgument:
		status = http.StatusBadRequest
		class = "invalid-argument"
	case codes.Unavailable:
		status = http.StatusServiceUnavailable
		class = "unavailable"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":       err.Error(),
		"error_class": class,
	})
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}
