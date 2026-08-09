package ocihost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
)

func (c *Common) PrepareContainerStatePaths(containerID string) (string, string, error) {
	if err := c.EnsureContainerPath(containerID); err != nil {
		return "", "", err
	}

	exitStatePath := c.RuntimeExitStatePath(containerID)
	pidFilePath := c.RuntimePIDFilePath(containerID)
	for _, path := range []string{exitStatePath, c.InitMonitorReadyStatePath(containerID), pidFilePath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", "", err
		}
	}
	return pidFilePath, exitStatePath, nil
}

type InitMonitorStartOptions struct {
	StdoutPath     string
	StderrPath     string
	ContainerID    string
	RuntimeArgs    []string
	RuntimePIDPath string
	ExitStatePath  string
	ReadyStatePath string
}

type initMonitorReadyState struct {
	Ready      bool      `json:"ready"`
	InitPID    int       `json:"initPid,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
	Error      string    `json:"error,omitempty"`
}

func (c *Common) StartCreateWithExitMonitor(ctx context.Context, options InitMonitorStartOptions) error {
	pidFilePath, exitStatePath, err := c.PrepareContainerStatePaths(options.ContainerID)
	if err != nil {
		return err
	}
	readyStatePath := c.InitMonitorReadyStatePath(options.ContainerID)
	options.RuntimePIDPath = pidFilePath
	options.ExitStatePath = exitStatePath
	options.ReadyStatePath = readyStatePath
	if c.initMonitorStarter != nil {
		return c.initMonitorStarter(ctx, options)
	}
	if c.runtimeRunnerBinary == "" {
		return fmt.Errorf("runtime runner binary is required")
	}
	cmdArgs := append([]string{
		"--runtime-binary", c.binary,
		"--exit-state", exitStatePath,
		"--pid-file", pidFilePath,
		"--monitor-init",
		"--ready-state", readyStatePath,
		"--",
	}, options.RuntimeArgs...)
	cmd := exec.Command(c.runtimeRunnerBinary, cmdArgs...)
	cleanupIO, err := ocicli.AttachCommandIO(cmd, options.StdoutPath, options.StderrPath)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		cleanupIO()
		return err
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
		cleanupIO()
		close(waitCh)
	}()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, ok, err := readInitMonitorReadyState(readyStatePath)
		if err != nil {
			_ = cmd.Process.Kill()
			<-waitCh
			return err
		}
		if ok {
			if !state.Ready {
				<-waitCh
				return fmt.Errorf("runtime init monitor failed before create: %s", state.Error)
			}
			if err := c.validateInitMonitorPID(options.ContainerID, state); err != nil {
				_ = cmd.Process.Kill()
				<-waitCh
				return err
			}
			return nil
		}

		select {
		case waitErr := <-waitCh:
			state, ok, readErr := readInitMonitorReadyState(readyStatePath)
			if readErr != nil {
				return readErr
			}
			if ok {
				if !state.Ready {
					return fmt.Errorf("runtime init monitor failed before create: %s", state.Error)
				}
				if err := c.validateInitMonitorPID(options.ContainerID, state); err != nil {
					return err
				}
				return nil
			}
			if waitErr != nil {
				return fmt.Errorf("runtime init monitor exited before create readiness: %w", waitErr)
			}
			return fmt.Errorf("runtime init monitor exited before create readiness without publishing a result")
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-waitCh
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Common) validateInitMonitorPID(containerID string, state initMonitorReadyState) error {
	runtimePID, err := c.RuntimePID(containerID)
	if err != nil {
		return err
	}
	if runtimePID != state.InitPID {
		return fmt.Errorf("runtime init monitor pid %d does not match OCI pid file %d", state.InitPID, runtimePID)
	}
	return nil
}

// AwaitInitMonitorExitState waits only when a successful create-time init
// monitor owns the container. Callers must complete this barrier after runtime
// delete and before removing allocation storage or monitor state; otherwise the
// monitor can lose the only durable place where it can publish the kernel wait
// status.
func (c *Common) AwaitInitMonitorExitState(ctx context.Context, containerID, runtimeName string) (bool, error) {
	state, ok, err := readInitMonitorReadyState(c.InitMonitorReadyStatePath(containerID))
	if err != nil {
		return false, err
	}
	if !ok || !state.Ready {
		return false, nil
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, ok, err := c.ReadExitState(containerID, runtimeName); err != nil {
			return true, fmt.Errorf("read init monitor exit state: %w", err)
		} else if ok {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return true, fmt.Errorf("wait for init monitor exit state: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func readInitMonitorReadyState(path string) (initMonitorReadyState, bool, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return initMonitorReadyState{}, false, nil
		}
		return initMonitorReadyState{}, false, fmt.Errorf("read runtime init monitor state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var state initMonitorReadyState
	if err := decoder.Decode(&state); err != nil {
		return initMonitorReadyState{}, false, fmt.Errorf("decode runtime init monitor state: %w", err)
	}
	if state.ObservedAt.IsZero() {
		return initMonitorReadyState{}, false, fmt.Errorf("runtime init monitor state is missing observedAt")
	}
	if state.Ready {
		if state.InitPID <= 0 || state.Error != "" {
			return initMonitorReadyState{}, false, fmt.Errorf("runtime init monitor ready state is malformed")
		}
	} else if state.InitPID != 0 || state.Error == "" || len(state.Error) > 1024 {
		return initMonitorReadyState{}, false, fmt.Errorf("runtime init monitor failure state is malformed")
	}
	return state, true, nil
}

func (c *Common) StartRunWithExitState(
	stdoutPath string,
	stderrPath string,
	containerID string,
	runtimeArgs []string,
) (<-chan error, error) {
	pidFilePath, exitStatePath, err := c.PrepareContainerStatePaths(containerID)
	if err != nil {
		return nil, err
	}
	if c.runtimeRunnerBinary == "" {
		return nil, fmt.Errorf("runtime runner binary is required")
	}

	cmdArgs := append([]string{
		"--runtime-binary", c.binary,
		"--exit-state", exitStatePath,
		"--pid-file", pidFilePath,
		"--",
	}, runtimeArgs...)
	cmd := exec.Command(c.runtimeRunnerBinary, cmdArgs...)
	cleanupIO, err := ocicli.AttachCommandIO(cmd, stdoutPath, stderrPath)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cleanupIO()
		return nil, err
	}

	waitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		cleanupIO()
		waitCh <- err
		close(waitCh)
	}()
	return waitCh, nil
}
