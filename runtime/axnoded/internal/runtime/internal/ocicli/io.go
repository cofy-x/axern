package ocicli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func OpenOutputFile(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
}

func AttachCommandIO(cmd *exec.Cmd, stdoutPath, stderrPath string) (func(), error) {
	stdoutFile, err := OpenOutputFile(stdoutPath)
	if err != nil {
		return nil, err
	}

	stderrFile := stdoutFile
	if stderrPath != stdoutPath {
		stderrFile, err = OpenOutputFile(stderrPath)
		if err != nil {
			if stdoutFile != nil {
				_ = stdoutFile.Close()
			}
			return nil, err
		}
	}

	if stdoutFile != nil {
		cmd.Stdout = stdoutFile
	}
	if stderrFile != nil {
		cmd.Stderr = stderrFile
	}

	return func() {
		if stderrFile != nil && stderrFile != stdoutFile {
			_ = stderrFile.Close()
		}
		if stdoutFile != nil {
			_ = stdoutFile.Close()
		}
	}, nil
}

func ReadOutputSnippet(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	const maxSnippet = 4096
	if len(data) > maxSnippet {
		data = data[len(data)-maxSnippet:]
	}
	return strings.TrimSpace(string(data))
}
