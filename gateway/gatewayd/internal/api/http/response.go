package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func statusFromError(err error) int {
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
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusBadRequest:
		return "invalid"
	case http.StatusUnauthorized:
		return "unauthorized"
	default:
		return "upstream"
	}
}
