package ocihost

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	"github.com/sirupsen/logrus"
)

type Executor interface {
	Execute(ctx context.Context, cmd string, args ...string) ([]byte, error)
}

// IOExecutor is required for OCI create because the container init process
// inherits its stdio from that command and retains the descriptors after the
// short-lived runtime CLI exits.
type IOExecutor interface {
	ExecuteWithIO(ctx context.Context, cmd, runtimeRoot, stdoutPath, stderrPath string, args ...string) error
}

type SystemExecutor struct{}

func (s *SystemExecutor) Execute(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	output, err := ocicli.ExecuteSystemCommand(ctx, cmd, args...)
	logrus.WithFields(logrus.Fields{
		"cmd":          cmd,
		"args_count":   len(args),
		"output_bytes": len(output),
		"error":        err != nil,
	}).Debug("executed runtime command")
	return output, err
}

func (s *SystemExecutor) ExecuteWithIO(ctx context.Context, cmd, runtimeRoot, stdoutPath, stderrPath string, args ...string) error {
	return ocicli.RunWithIO(ctx, cmd, runtimeRoot, stdoutPath, stderrPath, args...)
}
