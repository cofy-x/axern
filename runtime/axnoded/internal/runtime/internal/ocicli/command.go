package ocicli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// CommandError preserves the runtime command output alongside the process
// failure. OCI runtimes report semantic errors on stdout or stderr while the
// underlying *exec.ExitError only contains the exit status.
type CommandError struct {
	Err    error
	Output string
}

func (e *CommandError) Error() string {
	output := strings.TrimSpace(e.Output)
	if output == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%v: %s", e.Err, output)
}

func (e *CommandError) Unwrap() error { return e.Err }

// IsContainerNotFound reports only runtime errors that explicitly identify a
// missing container. Generic "not found" errors can describe a missing binary,
// bundle, or dependency and must not trigger storage cleanup.
func IsContainerNotFound(err error, containerID string) bool {
	var commandErr *CommandError
	containerID = strings.ToLower(strings.TrimSpace(containerID))
	if containerID == "" || !errors.As(err, &commandErr) {
		return false
	}
	output := strings.ToLower(commandErr.Output)
	for line := range strings.Lines(output) {
		line = strings.TrimSpace(line)
		// Current runc emits a scoped logrus error without echoing the ID for
		// commands such as `runc state <container-id>`. Accept only its complete
		// canonical message (plain or as the final structured msg field); a
		// broader substring match could misclassify a missing bundle or rootfs.
		if line == "container does not exist" || strings.HasSuffix(line, ` msg="container does not exist"`) {
			return true
		}
		if strings.Contains(line, "no such container: "+containerID) || strings.Contains(line, "no such container "+containerID) {
			return true
		}
		identities := []string{"container " + containerID, `container "` + containerID + `"`}
		for _, identity := range identities {
			if strings.Contains(line, identity+" does not exist") || strings.Contains(line, identity+" not found") {
				return true
			}
		}
	}
	return false
}

func EnsureRuntimeDirs(root, runtimeName string) (runtimeRoot string, containerRoot string, err error) {
	runtimeRoot = filepath.Join(root, runtimeName)
	logDir := filepath.Join(root, "logs")
	if err := os.MkdirAll(runtimeRoot, 0755); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", "", err
	}
	return runtimeRoot, filepath.Join(root, "containers"), nil
}

func CommandArgs(runtimeRoot string, args ...string) []string {
	return append([]string{"--root", runtimeRoot}, args...)
}

func NewCommandContext(ctx context.Context, binary, runtimeRoot string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, binary, CommandArgs(runtimeRoot, args...)...)
}

func ExecuteSystemCommand(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, cmd, args...)

	stdoutFile, err := os.CreateTemp("", "axnoded-cmd-stdout-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(stdoutFile.Name()) }()
	defer func() { _ = stdoutFile.Close() }()

	stderrFile, err := os.CreateTemp("", "axnoded-cmd-stderr-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(stderrFile.Name()) }()
	defer func() { _ = stderrFile.Close() }()

	command.Stdout = stdoutFile
	command.Stderr = stderrFile

	err = command.Run()

	stdoutData, readStdoutErr := os.ReadFile(stdoutFile.Name())
	stderrData, readStderrErr := os.ReadFile(stderrFile.Name())
	if readStdoutErr != nil {
		return nil, readStdoutErr
	}
	if readStderrErr != nil {
		return nil, readStderrErr
	}
	output := append(stdoutData, stderrData...)
	logrus.WithFields(logrus.Fields{
		"cmd":          cmd,
		"args_count":   len(args),
		"output_bytes": len(output),
		"error":        err != nil,
	}).Debug("executed system command")
	if err != nil {
		return output, &CommandError{Err: err, Output: string(output)}
	}
	return output, nil
}

func RunWithIO(ctx context.Context, binary, runtimeRoot, stdoutPath, stderrPath string, args ...string) error {
	cmdArgs := CommandArgs(runtimeRoot, args...)
	cmd := exec.CommandContext(ctx, binary, cmdArgs...)

	cleanupIO, err := AttachCommandIO(cmd, stdoutPath, stderrPath)
	if err != nil {
		return err
	}
	defer cleanupIO()

	err = cmd.Run()
	logrus.Debugf(
		"executed cmd with stdio: %s %s, stdout=%s, stderr=%s, err=%v",
		binary,
		cmdArgs,
		stdoutPath,
		stderrPath,
		err,
	)
	if err == nil {
		return nil
	}

	output := strings.TrimSpace(strings.Join([]string{
		ReadOutputSnippet(stdoutPath),
		ReadOutputSnippet(stderrPath),
	}, "\n"))
	if output == "" {
		return &CommandError{Err: err}
	}
	return &CommandError{Err: err, Output: output}
}
