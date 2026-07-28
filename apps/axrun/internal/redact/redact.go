package redact

import (
	"regexp"
	"strings"
)

const Replacement = "[REDACTED]"

var (
	bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	queryPattern  = regexp.MustCompile(`(?i)([?&][A-Za-z0-9_.~-]*(?:api[_-]?key|token|secret|credential|password|session)[A-Za-z0-9_.~-]*=)[^&\s'"<>]+`)
	assignPattern = regexp.MustCompile(`(?i)\b([A-Za-z0-9_.-]*(?:api[_-]?key|token|secret|credential|password|session)[A-Za-z0-9_.-]*=)[^\s'"<>]+`)
	jsonPattern   = regexp.MustCompile(`(?i)("?[A-Za-z0-9_.-]*(?:api[_-]?key|token|secret|credential|password|session)[A-Za-z0-9_.-]*"?\s*:\s*")[^"]*(")`)
)

func SensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "cookie", "set-cookie", "password":
		return true
	}
	return strings.Contains(normalized, "api-key") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "session")
}

func String(value string) string {
	if value == "" {
		return value
	}
	value = bearerPattern.ReplaceAllString(value, "Bearer "+Replacement)
	value = queryPattern.ReplaceAllString(value, "${1}"+Replacement)
	value = assignPattern.ReplaceAllString(value, "${1}"+Replacement)
	value = jsonPattern.ReplaceAllString(value, "${1}"+Replacement+"${2}")
	return value
}

func Header(values map[string][]string) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	sanitized := map[string][]string{}
	for key, entries := range values {
		if SensitiveKey(key) {
			continue
		}
		copied := make([]string, 0, len(entries))
		for _, entry := range entries {
			copied = append(copied, String(entry))
		}
		sanitized[key] = copied
	}
	if len(sanitized) == 0 {
		return nil
	}
	return sanitized
}

func Command(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	redacted := make([]string, len(argv))
	redactNext := false
	for i, arg := range argv {
		if redactNext {
			redacted[i] = Replacement
			redactNext = false
			continue
		}
		key, _, ok := strings.Cut(arg, "=")
		if ok && SensitiveKey(strings.TrimLeft(key, "-")) {
			redacted[i] = key + "=" + Replacement
			continue
		}
		if isSensitiveFlag(arg) {
			redacted[i] = arg
			redactNext = true
			continue
		}
		redacted[i] = String(arg)
	}
	return redacted
}

func isSensitiveFlag(arg string) bool {
	trimmed := strings.TrimLeft(strings.TrimSpace(arg), "-")
	if trimmed == "" {
		return false
	}
	return SensitiveKey(trimmed)
}
