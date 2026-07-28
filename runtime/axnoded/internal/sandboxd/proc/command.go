package proc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var DefaultCommandWait = 5 * time.Second

func RunShellOutput(ctx context.Context, waiter *Waiter, command string, extraEnv []string, timeout time.Duration) ([]byte, error) {
	return RunCommandOutput(ctx, waiter, "/bin/sh", []string{"-c", command}, extraEnv, timeout)
}

func RunCommandOutput(ctx context.Context, waiter *Waiter, name string, args []string, extraEnv []string, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = DefaultCommandWait
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.SysProcAttr = SysProcAttr()
	return RunStartedCommand(ctx, cmd, waiter)
}

func RunStartedCommand(ctx context.Context, cmd *exec.Cmd, waiter *Waiter) ([]byte, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}

	stdoutCh := readAll(stdoutPipe)
	stderrCh := readAll(stderrPipe)
	waitCh := waitForCommand(cmd, waiter)
	var timedOut error
	var result Result
	select {
	case result = <-waitCh:
	case <-ctx.Done():
		timedOut = ctx.Err()
		_ = KillProcessGroup(cmd.Process.Pid)
		result = <-waitCh
	}
	stdout := <-stdoutCh
	stderr := <-stderrCh
	if timedOut != nil {
		return nil, fmt.Errorf("%s: %w", strings.TrimSpace(stderr.text), timedOut)
	}
	if stdout.err != nil {
		return nil, fmt.Errorf("read stdout: %w", stdout.err)
	}
	if stderr.err != nil {
		return nil, fmt.Errorf("read stderr: %w", stderr.err)
	}
	if result.Err != nil {
		return nil, fmt.Errorf("%s: %w", strings.TrimSpace(stderr.text), result.Err)
	}
	if result.Signal != nil || result.ExitCode != 0 {
		return nil, fmt.Errorf("%s: exit code %d", strings.TrimSpace(stderr.text), result.ExitCode)
	}
	return stdout.data, nil
}

type streamRead struct {
	data []byte
	text string
	err  error
}

func readAll(reader io.Reader) <-chan streamRead {
	ch := make(chan streamRead, 1)
	go func() {
		data, err := io.ReadAll(reader)
		if errors.Is(err, os.ErrClosed) {
			err = nil
		}
		ch <- streamRead{data: data, text: string(data), err: err}
	}()
	return ch
}

func waitForCommand(cmd *exec.Cmd, waiter *Waiter) <-chan Result {
	if waiter != nil {
		return waiter.Watch(cmd)
	}
	ch := make(chan Result, 1)
	go func() {
		ch <- ResultFromError(cmd.Wait())
		close(ch)
	}()
	return ch
}
