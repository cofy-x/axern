package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type initMonitorConfig struct {
	runtimeBinary  string
	runtimeArgs    []string
	exitStatePath  string
	pidFilePath    string
	readyStatePath string
}

type monitorReadyState struct {
	Ready      bool      `json:"ready"`
	InitPID    int       `json:"initPid,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
	Error      string    `json:"error,omitempty"`
}

func runInitMonitor(cfg initMonitorConfig, stderr io.Writer) int {
	if err := enableChildSubreaper(); err != nil {
		return failInitMonitor(cfg.readyStatePath, fmt.Errorf("enable child subreaper: %w", err), stderr)
	}

	cmd := exec.Command(cfg.runtimeBinary, cfg.runtimeArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return failInitMonitor(cfg.readyStatePath, fmt.Errorf("create OCI container: %w", err), stderr)
	}

	initPID, err := readPositivePID(cfg.pidFilePath)
	if err != nil {
		return failInitMonitor(cfg.readyStatePath, err, stderr)
	}
	if err := persistJSONAtomically(cfg.readyStatePath, monitorReadyState{
		Ready:      true,
		InitPID:    initPID,
		ObservedAt: time.Now().UTC(),
	}, 0644); err != nil {
		fmt.Fprintf(stderr, "persist create-monitor readiness: %v\n", err)
		return 1
	}

	exitCode, err := waitForChildExit(initPID)
	if err != nil {
		fmt.Fprintf(stderr, "reap OCI init pid %d: %v\n", initPID, err)
		return 1
	}
	if err := persistExitState(cfg.exitStatePath, exitState{
		FinishedAt: time.Now().UTC(),
		ExitCode:   exitCode,
	}); err != nil {
		fmt.Fprintf(stderr, "persist runtime exit state: %v\n", err)
		return 1
	}
	return 0
}

func failInitMonitor(readyStatePath string, err error, stderr io.Writer) int {
	fmt.Fprintf(stderr, "runtime init monitor: %v\n", err)
	state := monitorReadyState{
		Ready:      false,
		ObservedAt: time.Now().UTC(),
		Error:      truncateMonitorError(err.Error()),
	}
	if persistErr := persistJSONAtomically(readyStatePath, state, 0644); persistErr != nil {
		fmt.Fprintf(stderr, "persist create-monitor failure: %v\n", persistErr)
	}
	return 1
}

func readPositivePID(path string) (int, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read OCI init pid file %q: %w", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(payload)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("OCI init pid file %q contains invalid pid %q", path, strings.TrimSpace(string(payload)))
	}
	return pid, nil
}

func truncateMonitorError(value string) string {
	const maxBytes = 1024
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes]
}

func decodeMonitorReadyState(payload []byte) (monitorReadyState, error) {
	var state monitorReadyState
	if err := json.Unmarshal(payload, &state); err != nil {
		return monitorReadyState{}, err
	}
	return state, nil
}
