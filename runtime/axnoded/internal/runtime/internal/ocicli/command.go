package ocicli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

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
	return output, err
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
		return err
	}
	return fmt.Errorf("%w: %s", err, output)
}
