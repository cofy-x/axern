package runtime

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCleanupOwnedRootfsStorageRetriesBusyUnmountBeforeRelease(t *testing.T) {
	removeCalls := 0
	releaseCalls := 0
	err := cleanupOwnedRootfsStorageWithInterval(
		context.Background(),
		"alloc-1",
		func(context.Context, string) error {
			removeCalls++
			if removeCalls < 3 {
				return syscall.EBUSY
			}
			return nil
		},
		func(string) error {
			releaseCalls++
			return nil
		},
		time.Millisecond,
	)
	require.NoError(t, err)
	require.Equal(t, 3, removeCalls)
	require.Equal(t, 1, releaseCalls)
}

func TestCleanupOwnedRootfsStorageRetainsReservationAfterBusyTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	releaseCalls := 0
	err := cleanupOwnedRootfsStorageWithInterval(
		ctx,
		"alloc-1",
		func(context.Context, string) error { return syscall.EBUSY },
		func(string) error {
			releaseCalls++
			return nil
		},
		time.Millisecond,
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, syscall.EBUSY)
	require.Zero(t, releaseCalls)
}

func TestCleanupOwnedRootfsStorageDoesNotRetryPermanentFailure(t *testing.T) {
	permanent := errors.New("invalid projection manifest")
	removeCalls := 0
	err := cleanupOwnedRootfsStorageWithInterval(
		context.Background(),
		"alloc-1",
		func(context.Context, string) error {
			removeCalls++
			return permanent
		},
		func(string) error { return nil },
		time.Millisecond,
	)
	require.ErrorIs(t, err, permanent)
	require.Equal(t, 1, removeCalls)
}
