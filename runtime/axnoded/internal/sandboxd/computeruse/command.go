package computeruse

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

var defaultCommandWait = proc.DefaultCommandWait

func runShellOutput(ctx context.Context, waiter *proc.Waiter, command string, extraEnv []string) ([]byte, error) {
	return runCommandOutput(ctx, waiter, "/bin/sh", []string{"-c", command}, extraEnv)
}

func runCommandOutput(ctx context.Context, waiter *proc.Waiter, name string, args []string, extraEnv []string) ([]byte, error) {
	output, err := proc.RunCommandOutput(ctx, waiter, name, args, extraEnv, defaultCommandWait)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCommandFailed, err)
	}
	return output, nil
}
