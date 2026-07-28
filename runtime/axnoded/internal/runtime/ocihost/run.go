package ocihost

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
)

func (c *Common) PrepareContainerStatePaths(containerID string) (string, string, error) {
	if err := c.EnsureContainerPath(containerID); err != nil {
		return "", "", err
	}

	exitStatePath := c.RuntimeExitStatePath(containerID)
	pidFilePath := c.RuntimePIDFilePath(containerID)
	for _, path := range []string{exitStatePath, pidFilePath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", "", err
		}
	}
	return pidFilePath, exitStatePath, nil
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
