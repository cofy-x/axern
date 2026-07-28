package ocicli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Exit struct {
	Timestamp time.Time
	Status    int
}

type PersistedExitState struct {
	ExitCode   int       `json:"exitCode"`
	FinishedAt time.Time `json:"finishedAt"`
}

func ParseWaitExitCode(output []byte) (int, error) {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return 0, nil
	}
	if code, err := strconv.Atoi(trimmed); err == nil {
		return code, nil
	}
	var payload struct {
		ExitStatus *int `json:"exitStatus"`
		ExitCode   *int `json:"exitCode"`
		Status     *int `json:"status"`
	}
	if err := json.Unmarshal(output, &payload); err == nil {
		switch {
		case payload.ExitStatus != nil:
			return *payload.ExitStatus, nil
		case payload.ExitCode != nil:
			return *payload.ExitCode, nil
		case payload.Status != nil:
			return *payload.Status, nil
		}
	}
	return 0, fmt.Errorf("unexpected OCI wait output: %q", trimmed)
}

func ReadPersistedExitState(path string, runtimeName string) (Exit, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Exit{}, false, nil
		}
		return Exit{}, true, err
	}

	var state PersistedExitState
	if err := json.Unmarshal(data, &state); err != nil {
		return Exit{}, true, fmt.Errorf("decode %s exit state: %w", runtimeName, err)
	}

	ts := state.FinishedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	return Exit{
		Timestamp: ts,
		Status:    state.ExitCode,
	}, true, nil
}

func PersistExitState(path string, exit Exit) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	payload, err := json.Marshal(PersistedExitState{
		ExitCode:   exit.Status,
		FinishedAt: exit.Timestamp.UTC(),
	})
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmpFile.Write(payload); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(0644); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Link(tmpPath, path); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return nil
}
