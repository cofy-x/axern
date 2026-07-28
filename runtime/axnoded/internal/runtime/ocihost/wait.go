package ocihost

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
)

func DecodeState[T any](runtimeName string, output []byte, out *T) error {
	if err := json.Unmarshal(output, out); err != nil {
		return fmt.Errorf("decode %s state output: %w", runtimeName, err)
	}
	return nil
}

func (c *Common) Wait(ctx context.Context, args ...string) (contract.Exit, bool, error) {
	output, err := c.Run(ctx, args...)
	if err != nil {
		return contract.Exit{}, false, err
	}
	status, parseErr := ParseWaitExitCode(output)
	if parseErr != nil {
		return contract.Exit{}, true, parseErr
	}
	return contract.Exit{Timestamp: time.Now(), Status: status}, true, nil
}

func (c *Common) ReadExitState(containerID string, runtimeName string) (contract.Exit, bool, error) {
	exit, ok, err := ocicli.ReadPersistedExitState(c.RuntimeExitStatePath(containerID), runtimeName)
	return contract.Exit{Timestamp: exit.Timestamp, Status: exit.Status}, ok, err
}

func (c *Common) PersistExitState(containerID string, exit contract.Exit) error {
	return ocicli.PersistExitState(c.RuntimeExitStatePath(containerID), ocicli.Exit{
		Timestamp: exit.Timestamp,
		Status:    exit.Status,
	})
}
