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

	var state struct {
		ExitCode   *int       `json:"exitCode"`
		FinishedAt *time.Time `json:"finishedAt"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return Exit{}, true, fmt.Errorf("decode %s exit state: %w", runtimeName, err)
	}
	if state.ExitCode == nil || state.FinishedAt == nil || state.FinishedAt.IsZero() {
		return Exit{}, true, fmt.Errorf("decode %s exit state: exitCode and finishedAt are required", runtimeName)
	}
	return Exit{
		Timestamp: *state.FinishedAt,
		Status:    *state.ExitCode,
	}, true, nil
}

func PersistExitState(path string, exit Exit) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("persisted exit state is not a regular file: %s", path)
		}
		if _, _, err := ReadPersistedExitState(path, "OCI"); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(path))
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
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Link(tmpPath, path); err != nil {
		if os.IsExist(err) {
			info, statErr := os.Lstat(path)
			if statErr != nil {
				return statErr
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("persisted exit state is not a regular file: %s", path)
			}
			if _, _, readErr := ReadPersistedExitState(path, "OCI"); readErr != nil {
				return readErr
			}
			return syncDirectory(dir)
		}
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(dir string) error {
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := dirFile.Sync()
	closeErr := dirFile.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
