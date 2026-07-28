package llmproxy

import "strings"

func splitLines(value string) []string {
	return strings.Split(value, "\n")
}

func trimSpace(value string) string {
	return strings.TrimSpace(value)
}
