package startupflow

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type Options struct {
	RuntimeName    string
	ContainerID    string
	PIDFilePath    string
	WaitCh         <-chan error
	ReadyByState   func(context.Context) bool
	ExitState      func() (contract.Exit, bool, error)
	UnreadableExit func(string, string, error) error
}

func Wait(ctx context.Context, options Options) error {
	if strings.TrimSpace(options.PIDFilePath) == "" {
		return fmt.Errorf("%s startup pid file path is required for %s", options.RuntimeName, options.ContainerID)
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, err := os.Stat(options.PIDFilePath); err == nil {
			return nil
		}
		if options.ReadyByState != nil && options.ReadyByState(ctx) {
			return nil
		}
		if ok, err := acceptExitState(options); ok || err != nil {
			return err
		}

		if options.WaitCh == nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-options.WaitCh:
			if options.ReadyByState != nil && options.ReadyByState(context.Background()) {
				return nil
			}
			if ok, exitErr := acceptExitState(options); ok || exitErr != nil {
				return exitErr
			}
			if err == nil {
				return fmt.Errorf("%s run exited before startup handshake completed", options.RuntimeName)
			}
			return fmt.Errorf("%s run exited before container started: %w", options.RuntimeName, err)
		case <-ticker.C:
		}
	}
}

func acceptExitState(options Options) (bool, error) {
	if options.ExitState == nil {
		return false, nil
	}
	_, ok, err := options.ExitState()
	if err != nil {
		if options.UnreadableExit != nil {
			return false, options.UnreadableExit(options.RuntimeName, options.ContainerID, err)
		}
		return false, err
	}
	if !ok {
		return false, nil
	}
	return true, nil
}

func UnreadableExitError(runtimeName, containerID string, err error) error {
	return fmt.Errorf("%s startup exit state is unreadable for %s: %w", runtimeName, containerID, err)
}
