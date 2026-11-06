package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	"github.com/stretchr/testify/require"
)

type recoveryExecutor struct {
	recordingExecutor
	status           string
	fence            string
	transitionErr    error
	unconfirmed      bool
	beforeTransition func()
}

func (e *recoveryExecutor) Execute(ctx context.Context, binary string, args ...string) ([]byte, error) {
	_, _ = e.recordingExecutor.Execute(ctx, binary, args...)
	if containsArg(args, "state") {
		return []byte(fmt.Sprintf(`{"status":%q,"pid":1234}`, e.status)), nil
	}
	for _, op := range []string{"pause", "resume", "start"} {
		if !containsArg(args, op) {
			continue
		}
		if _, err := os.Stat(e.fence); err != nil {
			return nil, fmt.Errorf("transition lacks durable fence: %w", err)
		}
		if e.beforeTransition != nil {
			e.beforeTransition()
		}
		if e.transitionErr != nil {
			return nil, e.transitionErr
		}
		if !e.unconfirmed {
			e.status = map[string]string{"pause": "paused", "resume": "running", "start": "running"}[op]
		}
	}
	return nil, nil
}

func TestRunscRecoveryTransitionsFenceRPCAndConfirmState(t *testing.T) {
	for _, from := range []string{"running", "paused"} {
		t.Run(from, func(t *testing.T) {
			h := newNativeRecoveryTestHandler(t)
			fence := filepath.Join(filepath.Dir(h.containerRoot), "runtime-transition")
			executor := &recoveryExecutor{status: from, fence: fence}
			executor.beforeTransition = func() {
				info, err := os.Stat(fence)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			}
			h.common.SetExecutor(executor)
			var err error
			if from == "running" {
				err = h.PauseContainer(t.Context(), contract.HandlerOptions{ContainerID: "allocation"})
			} else {
				err = h.ResumeContainer(t.Context(), contract.HandlerOptions{ContainerID: "allocation"})
			}
			require.NoError(t, err)
			require.NoFileExists(t, fence)
			require.NotEqual(t, from, executor.status)
		})
	}
}

func TestRunscRecoveryTransitionFailureRetainsFence(t *testing.T) {
	for _, unconfirmed := range []bool{false, true} {
		t.Run(fmt.Sprint(unconfirmed), func(t *testing.T) {
			h := newNativeRecoveryTestHandler(t)
			fence := filepath.Join(filepath.Dir(h.containerRoot), "runtime-transition")
			e := &recoveryExecutor{status: "paused", fence: fence, unconfirmed: unconfirmed}
			if !unconfirmed {
				e.transitionErr = errors.New("RPC outcome unavailable")
			}
			h.common.SetExecutor(e)
			require.Error(t, h.ResumeContainer(t.Context(), contract.HandlerOptions{ContainerID: "allocation"}))
			require.FileExists(t, fence)
			count := len(e.Args())
			require.ErrorContains(t, h.PauseContainer(t.Context(), contract.HandlerOptions{ContainerID: "allocation"}), "unfinished runtime transition")
			require.Len(t, e.Args(), count, "an unfinished operation must block even idempotent-looking persisted state")
		})
	}
}

func TestRuntimeTransitionFenceRejectsExistingFileAndSymlink(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(fmt.Sprint(symlink), func(t *testing.T) {
			dir := t.TempDir()
			fence := filepath.Join(dir, "runtime-transition")
			if symlink {
				require.NoError(t, os.Symlink(filepath.Join(dir, "target"), fence))
			} else {
				require.NoError(t, os.WriteFile(fence, []byte("existing"), 0o600))
			}
			require.Error(t, createRuntimeTransitionFence(fence))
			require.NoFileExists(t, filepath.Join(dir, "target"))
		})
	}
}

func TestPausedFailStopUsesAdmittedCgroupWithoutResumeOrDeletion(t *testing.T) {
	h, manifest, executor := taskEnforcementFixture(t)
	executor.status = "paused"
	previous, previousStopped := killRecoveryCgroup, recoveryCgroupStopped
	t.Cleanup(func() { killRecoveryCgroup = previous; recoveryCgroupStopped = previousStopped })
	stopped := false
	recoveryCgroupStopped = func(string, int, string, string, uint64) (bool, error) { return stopped, nil }
	called := false
	killRecoveryCgroup = func(path string, pid int, binary, boot, mount string, inode uint64) error {
		called = true
		require.Equal(t, manifest.GetRuntimeCgroupPath(), path)
		require.Equal(t, 1234, pid)
		require.Equal(t, "/fake/runsc", binary)
		stopped = true
		return nil
	}
	require.NoError(t, h.KillSandbox(t.Context(), contract.HandlerOptions{ContainerID: "task-allocation", EnforcementManifest: manifest}))
	require.True(t, called)
	require.Equal(t, "paused", executor.status, "pinned runsc leaves this status stale after host kill")
	confirmed, err := h.VerifySandboxStopped(t.Context(), contract.HandlerOptions{ContainerID: "task-allocation", EnforcementManifest: manifest})
	require.NoError(t, err)
	require.True(t, confirmed)
	h.newWorkloadClient = func(string) sandboxdWorkloadClient {
		return &workloadClientStub{err: errors.New("sandboxd socket closed")}
	}
	_, err = h.Wait(t.Context(), contract.HandlerOptions{ContainerID: "task-allocation", EnforcementManifest: manifest})
	require.ErrorIs(t, err, contract.ErrExitStatusUnavailable, "kernel-proven exit must pass the ordinary terminal checkpoint barrier")
	for _, args := range executor.Args() {
		require.NotContains(t, args, "resume")
		require.NotContains(t, args, "kill", "pinned runsc cannot signal paused guests")
		require.NotContains(t, args, "delete")
	}
	require.FileExists(t, filepath.Join(manifest.BundlePath, runtimeEnforcementManifestFile))
	// Native cleanup must never delegate cached host PIDs back to runsc.
	h.nativeRecovery = true
	require.NoError(t, h.deleteNativeRuntimeContainer(t.Context(), contract.HandlerOptions{ContainerID: "task-allocation", EnforcementManifest: manifest}, true))
	for _, args := range executor.Args() {
		require.NotContains(t, args, "delete")
		require.NotContains(t, args, "kill")
	}
}

func TestPausedFailStopRejectsMissingOrMismatchedAuthority(t *testing.T) {
	h, manifest, executor := taskEnforcementFixture(t)
	executor.status = "paused"
	require.Error(t, h.KillSandbox(t.Context(), contract.HandlerOptions{ContainerID: "task-allocation"}))
	manifest.RuntimeCgroupPath = "/other/workload"
	require.Error(t, h.KillSandbox(t.Context(), contract.HandlerOptions{ContainerID: "task-allocation", EnforcementManifest: manifest}))
	require.Equal(t, "paused", executor.status)
}

func TestPausedTaskRecoveryReadsControlsWithoutResuming(t *testing.T) {
	h, manifest, executor := taskEnforcementFixture(t)
	executor.status = "paused"
	require.NoError(t, h.VerifyAllocationTaskLimits(t.Context(), "task-allocation", manifest))
	for _, args := range executor.Args() {
		require.NotContains(t, args, "resume")
		require.NotContains(t, args, "write-control")
	}
	accepted, err := h.acceptWaitExit(t.Context(), "task-allocation", contract.Exit{Status: 0})
	require.NoError(t, err)
	require.False(t, accepted, "paused runtime is not terminal evidence")
}

type delayedRecoveryStateExecutor struct{ recordingExecutor }

func (e *delayedRecoveryStateExecutor) Execute(ctx context.Context, binary string, args ...string) ([]byte, error) {
	_, _ = e.recordingExecutor.Execute(ctx, binary, args...)
	if containsArg(args, "state") {
		<-ctx.Done()
		return []byte(`{"status":"paused","pid":1234}`), nil
	}
	return nil, nil
}
func TestResumeNeverIssuesRPCWhenStateReadCrossesLeaseDeadline(t *testing.T) {
	h := newNativeRecoveryTestHandler(t)
	e := &delayedRecoveryStateExecutor{}
	h.common.SetExecutor(e)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, h.ResumeContainer(ctx, contract.HandlerOptions{ContainerID: "allocation"}), context.DeadlineExceeded)
	require.NoFileExists(t, filepath.Join(filepath.Dir(h.containerRoot), "runtime-transition"))
	for _, args := range e.Args() {
		require.NotContains(t, args, "resume")
	}
}
func TestStartFencesCreatedStatusUntilRunningPublication(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			h := newNativeRecoveryTestHandler(t)
			fence := filepath.Join(filepath.Dir(h.containerRoot), "runtime-transition")
			e := &recoveryExecutor{status: "created", fence: fence}
			if uncertain {
				e.transitionErr = errors.New("state publication failed")
				e.beforeTransition = func() { e.status = "running" }
			}
			h.common.SetExecutor(e)
			err := h.startWithTransitionFence(t.Context(), "allocation", h.lifecycleArgs("start", "allocation"))
			if uncertain {
				require.Error(t, err)
				require.FileExists(t, fence)
				require.ErrorContains(t, h.PauseContainer(t.Context(), contract.HandlerOptions{ContainerID: "allocation"}), "unfinished runtime transition")
			} else {
				require.NoError(t, err)
				require.NoFileExists(t, fence)
			}
		})
	}
}

func newNativeRecoveryTestHandler(t *testing.T) *RunscServiceHandler {
	h := newRunscWorkloadTestHandler(t, &workloadClientStub{})
	h.nativeRecovery = true
	return h
}

func TestDefaultStartFailureDoesNotWriteNativeTransitionFence(t *testing.T) {
	h := newRunscWorkloadTestHandler(t, &workloadClientStub{})
	require.False(t, h.nativeRecovery)
	h.common.SetExecutor(&scriptedExecutor{errors: map[string][]error{"": {errors.New("ordinary start failed")}}})
	_, err := h.StartPreparedContainer(t.Context(), &contract.PreparedContainer{ContainerID: "allocation", Metadata: &apipb.ContainerMetadata{}}, contract.HandlerOptions{ContainerID: "allocation"})
	require.ErrorContains(t, err, "ordinary start failed")
	require.NoFileExists(t, filepath.Join(filepath.Dir(h.containerRoot), "runtime-transition"))
	require.ErrorContains(t, h.PauseContainer(t.Context(), contract.HandlerOptions{ContainerID: "allocation"}), "native recovery is disabled")
}

func TestNativeDeleteWithoutAuthorityNeverSignalsCachedPIDs(t *testing.T) {
	for _, state := range []string{"created", "running", "paused", "stopped", "unknown"} {
		for _, force := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/force=%t", state, force), func(t *testing.T) {
				h := newNativeRecoveryTestHandler(t)
				executor := &recoveryExecutor{status: state}
				h.common.SetExecutor(executor)
				require.ErrorContains(t, h.deleteRuntimeContainer(t.Context(), "allocation", force), "requires admitted enforcement ownership")
				for _, args := range executor.Args() {
					require.NotContains(t, args, "kill")
					require.NotContains(t, args, "delete")
				}
			})
		}
	}
}

func TestNativeDeleteRejectsInvalidIdentityBeforeRuntimeLookup(t *testing.T) {
	h := newNativeRecoveryTestHandler(t)
	executor := &recoveryExecutor{status: "stopped"}
	h.common.SetExecutor(executor)
	for _, id := range []string{"", "..", "../other", "/other"} {
		_, err := h.DeleteContainer(t.Context(), &apipb.DeleteContainerRequest{ID: id}, contract.HandlerOptions{ContainerID: id})
		require.ErrorContains(t, err, "exact allocation identity")
	}
	require.Empty(t, executor.Args())
}

func TestNativeDeleteIsIdempotentAfterExactStateRetirement(t *testing.T) {
	h := newNativeRecoveryTestHandler(t)
	h.common.SetExecutor(&scriptedExecutor{errors: map[string][]error{"state": {
		&ocicli.CommandError{Err: errors.New("exit status 1"), Output: "container allocation does not exist"},
	}}})
	// The bundle projection may already have been reclaimed too. Exact runtime
	// absence remains sufficient to finish previously authorized storage cleanup.
	require.NoError(t, h.deleteNativeRuntimeContainer(t.Context(), contract.HandlerOptions{ContainerID: "allocation", EnforcementManifest: &apipb.AllocationEnforcementManifest{}}, true))
}
