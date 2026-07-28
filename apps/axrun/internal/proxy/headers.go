package proxy

import (
	"net/http"

	"github.com/cofy-x/axern/apps/axrun/internal/redact"
)

// SanitizeHeaders returns a copy of headers with sensitive entries removed.
func SanitizeHeaders(headers http.Header) map[string][]string {
	return redact.Header(headers)
}
