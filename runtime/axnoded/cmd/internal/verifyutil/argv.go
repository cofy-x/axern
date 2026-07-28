package verifyutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ResolveArgv returns argv-json when provided; otherwise it falls back to a
// shell snippet executed by /bin/sh -c.
func ResolveArgv(argvJSON, shellSnippet, defaultShellSnippet string) ([]string, error) {
	if strings.TrimSpace(argvJSON) != "" {
		var argv []string
		if err := json.Unmarshal([]byte(argvJSON), &argv); err != nil {
			return nil, fmt.Errorf("parse argv-json: %w", err)
		}
		if len(argv) == 0 {
			return nil, fmt.Errorf("argv-json must contain at least one argv element")
		}
		return argv, nil
	}

	command := shellSnippet
	if command == "" {
		command = defaultShellSnippet
	}
	return []string{"/bin/sh", "-c", command}, nil
}
