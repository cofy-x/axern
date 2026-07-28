package startupflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitReturnsWhenPIDFileExists(t *testing.T) {
	pidFilePath := filepath.Join(t.TempDir(), "runtime.pid")
	require.NoError(t, os.WriteFile(pidFilePath, []byte("123"), 0644))

	err := Wait(context.Background(), Options{
		RuntimeName: "runc",
		ContainerID: "axctl-test",
		PIDFilePath: pidFilePath,
	})

	assert.NoError(t, err)
}

func TestWaitReportsSupervisorExitBeforeReady(t *testing.T) {
	waitCh := make(chan error, 1)
	waitCh <- errors.New("exit status 7")
	close(waitCh)

	err := Wait(context.Background(), Options{
		RuntimeName: "runc",
		ContainerID: "axctl-test",
		PIDFilePath: filepath.Join(t.TempDir(), "missing.pid"),
		WaitCh:      waitCh,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runc run exited before container started")
	assert.Contains(t, err.Error(), "exit status 7")
}

func TestWaitReportsSupervisorCleanExitBeforeReady(t *testing.T) {
	waitCh := make(chan error, 1)
	waitCh <- nil
	close(waitCh)

	err := Wait(context.Background(), Options{
		RuntimeName: "runc",
		ContainerID: "axctl-test",
		PIDFilePath: filepath.Join(t.TempDir(), "missing.pid"),
		WaitCh:      waitCh,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runc run exited before startup handshake completed")
}

func TestWaitAcceptsPersistedRuntimeExitBeforeReady(t *testing.T) {
	err := Wait(context.Background(), Options{
		RuntimeName: "runsc",
		ContainerID: "axctl-test",
		PIDFilePath: filepath.Join(t.TempDir(), "missing.pid"),
		ExitState: func() (contract.Exit, bool, error) {
			return contract.Exit{Status: 7}, true, nil
		},
	})

	assert.NoError(t, err)
}

func TestWaitAcceptsCleanPersistedRuntimeExitBeforeReady(t *testing.T) {
	err := Wait(context.Background(), Options{
		RuntimeName: "runsc",
		ContainerID: "axctl-test",
		PIDFilePath: filepath.Join(t.TempDir(), "missing.pid"),
		ExitState: func() (contract.Exit, bool, error) {
			return contract.Exit{Status: 0}, true, nil
		},
	})

	assert.NoError(t, err)
}

func TestWaitAcceptsReadyStateAfterSupervisorExit(t *testing.T) {
	waitCh := make(chan error, 1)
	waitCh <- nil
	close(waitCh)

	err := Wait(context.Background(), Options{
		RuntimeName: "runc",
		ContainerID: "axctl-test",
		PIDFilePath: filepath.Join(t.TempDir(), "missing.pid"),
		WaitCh:      waitCh,
		ReadyByState: func(context.Context) bool {
			return true
		},
	})

	assert.NoError(t, err)
}

func TestWaitAcceptsCleanPersistedRuntimeExitAfterSupervisorExit(t *testing.T) {
	waitCh := make(chan error, 1)
	waitCh <- nil
	close(waitCh)
	exitStateCalls := 0

	err := Wait(context.Background(), Options{
		RuntimeName: "runsc",
		ContainerID: "axctl-test",
		PIDFilePath: filepath.Join(t.TempDir(), "missing.pid"),
		WaitCh:      waitCh,
		ExitState: func() (contract.Exit, bool, error) {
			exitStateCalls++
			return contract.Exit{Status: 0}, true, nil
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, exitStateCalls)
}

func TestWaitAcceptsPersistedRuntimeExitAfterSupervisorExit(t *testing.T) {
	waitCh := make(chan error, 1)
	waitCh <- nil
	close(waitCh)

	err := Wait(context.Background(), Options{
		RuntimeName: "runsc",
		ContainerID: "axctl-test",
		PIDFilePath: filepath.Join(t.TempDir(), "missing.pid"),
		WaitCh:      waitCh,
		ExitState: func() (contract.Exit, bool, error) {
			return contract.Exit{Status: 7}, true, nil
		},
	})

	assert.NoError(t, err)
}

func TestWaitUsesUnreadableExitMapper(t *testing.T) {
	errUnreadable := errors.New("bad exit json")

	err := Wait(context.Background(), Options{
		RuntimeName: "runsc",
		ContainerID: "axctl-test",
		PIDFilePath: filepath.Join(t.TempDir(), "missing.pid"),
		ExitState: func() (contract.Exit, bool, error) {
			return contract.Exit{}, false, errUnreadable
		},
		UnreadableExit: UnreadableExitError,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runsc startup exit state is unreadable for axctl-test")
	assert.True(t, errors.Is(err, errUnreadable))
}

func TestWaitReturnsContextErrorWithoutWaitChannel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	err := Wait(ctx, Options{
		RuntimeName: "runsc",
		ContainerID: "axctl-test",
		PIDFilePath: filepath.Join(t.TempDir(), "missing.pid"),
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)
}

func TestWaitRequiresPIDFilePath(t *testing.T) {
	err := Wait(context.Background(), Options{
		RuntimeName: "runsc",
		ContainerID: "axctl-test",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runsc startup pid file path is required for axctl-test")
}

func TestUnreadableExitErrorFormatsRuntimeAndContainer(t *testing.T) {
	err := UnreadableExitError("runsc", "axctl-test", errors.New("decode failed"))

	assert.Contains(t, err.Error(), "runsc startup exit state is unreadable for axctl-test")
}
