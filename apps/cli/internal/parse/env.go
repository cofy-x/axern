package parse

import (
	"fmt"
	"strings"
)

func EnvFlags(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid env %q, want KEY=VALUE", value)
		}
		out[strings.TrimSpace(key)] = val
	}
	return out, nil
}

func Labels(values []string) map[string]string {
	labels := map[string]string{}
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		labels[key] = strings.TrimSpace(val)
	}
	return labels
}
