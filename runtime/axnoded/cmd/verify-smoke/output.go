package main

import (
	"fmt"
	"os"
	"strings"
)

func verifyOutputs(stdoutPath, stderrPath, expectStdout, expectStderr string) error {
	stdoutData, err := os.ReadFile(stdoutPath)
	if err != nil {
		return fmt.Errorf("read stdout: %w", err)
	}
	if !strings.Contains(string(stdoutData), expectStdout) {
		return fmt.Errorf("stdout missing expected marker: %q", string(stdoutData))
	}

	stderrData, err := os.ReadFile(stderrPath)
	if err != nil {
		return fmt.Errorf("read stderr: %w", err)
	}
	if expectStderr != "" && !strings.Contains(string(stderrData), expectStderr) {
		return fmt.Errorf("stderr missing expected marker: %q", string(stderrData))
	}
	return nil
}
