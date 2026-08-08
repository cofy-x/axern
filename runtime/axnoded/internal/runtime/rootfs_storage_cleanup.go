package runtime

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
)

const (
	rootfsViewCleanupTimeout       = 30 * time.Second
	rootfsViewCleanupRetryInterval = 100 * time.Millisecond
)

func rootfsViewCleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), rootfsViewCleanupTimeout)
}

func cleanupOwnedRootfsStorage(
	containerID string,
	removeView func(context.Context, string) error,
	releaseReservation func(string) error,
) error {
	cleanupCtx, cancel := rootfsViewCleanupContext()
	defer cancel()
	return cleanupOwnedRootfsStorageWithInterval(cleanupCtx, containerID, removeView, releaseReservation, rootfsViewCleanupRetryInterval)
}

func cleanupOwnedRootfsStorageWithInterval(
	ctx context.Context,
	containerID string,
	removeView func(context.Context, string) error,
	releaseReservation func(string) error,
	retryInterval time.Duration,
) error {
	for {
		err := removeView(ctx, containerID)
		if err == nil {
			return releaseReservation(containerID)
		}
		if !errors.Is(err, syscall.EBUSY) {
			// The upper may still be mounted or referenced by a live runtime. Keep
			// its reservation until reconciliation can prove it is safe to release.
			return err
		}

		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for runtime rootfs references to drain: %w", errors.Join(err, ctx.Err()))
		case <-timer.C:
		}
	}
}
