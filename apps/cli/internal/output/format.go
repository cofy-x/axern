package output

import (
	"fmt"
	"strings"
)

type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

func ParseFormat(value string, allowed ...Format) (Format, error) {
	format := Format(strings.ToLower(strings.TrimSpace(value)))
	if format == "" {
		format = FormatTable
	}
	if len(allowed) == 0 {
		allowed = []Format{FormatTable, FormatJSON}
	}
	for _, candidate := range allowed {
		if format == candidate {
			return format, nil
		}
	}
	return "", fmt.Errorf("invalid output format %q, want one of: %s", value, formatList(allowed))
}

func formatList(formats []Format) string {
	parts := make([]string, 0, len(formats))
	for _, format := range formats {
		parts = append(parts, string(format))
	}
	return strings.Join(parts, ", ")
}
