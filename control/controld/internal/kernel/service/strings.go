package servicekernel

import "strings"

func FirstNonEmpty(values ...string) string {
	return firstNonEmpty(values...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
