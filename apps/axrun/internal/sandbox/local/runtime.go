package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

type Runtime struct{}

func (Runtime) Preflight() error {
	return nil
}

func (Runtime) Create(context.Context) (sandbox.Instance, error) {
	root, err := os.MkdirTemp("", "axrun-local-sandbox-*")
	if err != nil {
		return nil, err
	}
	return instance{root: root}, nil
}

type instance struct {
	root string
}

func (i instance) Exec(ctx context.Context, command sandbox.ExecCommand, options sandbox.ExecOptions) (sandbox.ExecResult, error) {
	name, args, err := execCommand(command)
	if err != nil {
		return sandbox.ExecResult{}, err
	}
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if options.CWD != "" {
		dir := i.mapPath(options.CWD)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return sandbox.ExecResult{}, fmt.Errorf("create cwd %s: %w", options.CWD, err)
		}
		cmd.Dir = dir
	}
	cmd.Env = mergedEnv(options.Env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	result := sandbox.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() != nil {
		result.ExitCode = -1
		return result, nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, err
	}
	return result, nil
}

func execCommand(command sandbox.ExecCommand) (string, []string, error) {
	if err := command.Validate(); err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(command.Shell()) != "" {
		return shellName(), shellArgs(command.Shell()), nil
	}
	argv := command.Argv()
	return argv[0], argv[1:], nil
}

func mergedEnv(extra map[string]string) []string {
	env := os.Environ()
	for key, value := range extra {
		if strings.TrimSpace(key) == "" {
			continue
		}
		env = append(env, key+"="+value)
	}
	return env
}

func (instance) State() (sandbox.State, error) {
	return sandbox.State{}, nil
}

func (i instance) Close(context.Context) error {
	return os.RemoveAll(i.root)
}

func (i instance) PathExists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(i.mapPath(path))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (i instance) mapPath(path string) string {
	if path == "" {
		return path
	}
	if filepath.IsAbs(path) {
		return filepath.Join(i.root, strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)))
	}
	return path
}

func shellName() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}

func shellArgs(command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"/C", command}
	}
	return []string{"-c", command}
}
