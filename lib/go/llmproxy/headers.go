package llmproxy

import (
	"net/http"
	"strings"
)

func SanitizeHeaders(headers http.Header) map[string][]string {
	if headers == nil {
		return nil
	}
	out := map[string][]string{}
	for key, values := range headers {
		if isSensitiveHeader(key) {
			out[key] = []string{"<redacted>"}
			continue
		}
		out[key] = append([]string(nil), values...)
	}
	return out
}

func isSensitiveHeader(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "authorization", "proxy-authorization", "x-api-key", "cookie", "set-cookie":
		return true
	}
	return strings.Contains(normalized, "api-key") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "session")
}

func RemoveHopHeaders(h http.Header) {
	for _, key := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Connection",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		h.Del(key)
	}
}
