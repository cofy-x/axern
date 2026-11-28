package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func statusFromError(err error) int {
	switch status.Code(err) {
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	case codes.FailedPrecondition:
		return http.StatusGone
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "not found"):
		return http.StatusNotFound
	case strings.Contains(text, "no ready endpoints"), strings.Contains(text, "unavailable"):
		return http.StatusServiceUnavailable
	case strings.Contains(text, "invalid"):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

func errorClassFromStatus(status int) string {
	switch status {
	case http.StatusNotFound:
		return "not_found"
	case http.StatusGone:
		return "inactive"
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusBadRequest:
		return "invalid"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	default:
		return "upstream"
	}
}
