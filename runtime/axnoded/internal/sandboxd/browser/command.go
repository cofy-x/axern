package browser

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

var defaultHookWait = proc.DefaultCommandWait

func runHook(ctx context.Context, waiter *proc.Waiter, command string, targetURL string) error {
	if _, err := proc.RunShellOutput(ctx, waiter, command, []string{"AXERN_BROWSER_URL=" + targetURL}, defaultHookWait); err != nil {
		return fmt.Errorf("%w: %w", ErrCommandFailed, err)
	}
	return nil
}
