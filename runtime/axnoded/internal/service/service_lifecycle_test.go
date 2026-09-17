package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/require"
)

type deleteCountingRuntime struct {
	*runtimetest.FakeSandboxRuntime
	deleteCalls atomic.Int64
}

func (r *deleteCountingRuntime) DeleteContainer(ctx context.Context, request *runtimeapi.DeleteContainerRequest, options contract.HandlerOptions) (*runtimeapi.DeleteContainerResponse, error) {
	r.deleteCalls.Add(1)
	return r.FakeSandboxRuntime.DeleteContainer(ctx, request, options)
}

func TestRunReturnsWithoutBlocking(t *testing.T) {
	s := newTestService(t,
		runtimetest.NewFakeSandboxRuntime(),
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, s.Shutdown(ctx))
	})

	done := make(chan error, 1)
	go func() {
		done <- s.Run(t.Context())
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run blocked instead of returning after starting background loops")
	}
}

func TestRunMarksServiceReadyAfterInitialHousekeeping(t *testing.T) {
	s := newTestService(t,
		runtimetest.NewFakeSandboxRuntime(),
	)
	s.ready.Store(false)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, s.Shutdown(ctx))
	})

	require.NoError(t, s.Run(t.Context()))

	require.Eventually(t, s.Ready, 2*time.Second, 50*time.Millisecond)
}

func TestShutdownDrainsRetainedEnvironments(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires linux resource manager setup")
	}

	s := newTestService(t,
		runtimetest.NewFakeSandboxRuntime(),
	)

	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	require.NoError(t, os.MkdirAll(rootfsDir, 0o755))

	fr := &runtimeapi.ResolvedEnvironment{
		ID: "retained-on-close",
		Rootfs: &runtimeapi.RootfsConfig{
			Type:   runtimeapi.RootfsSrcType_LOCAL,
			Source: &runtimeapi.RootfsConfig_Path{Path: rootfsDir},
		},
		Argv: []string{"/bin/sh"},
	}
	rootfsCfg, err := environmentcache.RootfsConfigFromResolvedEnvironment(fr)
	require.NoError(t, err)
	result, err := s.environmentCache.PrepareEnvironment(t.Context(), fr, rootfsCfg)
	require.NoError(t, err)
	lr := result.Environment

	lr.IncRef()
	lr.DecRef()
	require.True(t, lr.Retained())

	require.NoError(t, s.Shutdown(t.Context()))
	require.Nil(t, s.environmentCache.GetPreparedEnvironment("retained-on-close"))
	require.True(t, lr.Released())
}

func TestShutdownPreservesLiveAllocationForRestartRecovery(t *testing.T) {
	runtimeHandler := &deleteCountingRuntime{FakeSandboxRuntime: runtimetest.NewFakeSandboxRuntime()}
	s := newTestService(t, runtimeHandler)
	const allocationID = "allocation-survives-restart"
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.NoError(t, s.allocations.StoreAllocationIntent(allocationID, "node-a", digest, time.Now().Add(time.Minute), nil, nil, nil))
	s.containerManager.StoreMetadata(allocationID, &runtimeapi.ContainerMetadata{})
	markTestContainerRunning(t, s, allocationID)

	require.NoError(t, s.Shutdown(t.Context()))
	require.Zero(t, runtimeHandler.deleteCalls.Load(), "graceful process shutdown must not become Allocation deletion")
	require.True(t, s.allocations.HasAllocation(allocationID), "durable Allocation recovery record must survive process shutdown")
}

func TestExpiredExecutionLeaseStopsAllocation(t *testing.T) {
	runtimeHandler := &deleteCountingRuntime{FakeSandboxRuntime: runtimetest.NewFakeSandboxRuntime()}
	s := newTestService(t, runtimeHandler)
	const allocationID = "allocation-expired"
	const digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	now := time.Now().UTC()
	require.NoError(t, s.allocations.StoreAllocationIntent(allocationID, "node-a", digest, now.Add(time.Minute), nil, nil, nil))
	s.containerManager.StoreMetadata(allocationID, &runtimeapi.ContainerMetadata{})
	markTestContainerRunning(t, s, allocationID)
	require.NoError(t, s.allocations.RenewExecutionLeases(nil, now))

	s.stopExpiredExecutionLeases(t.Context(), now.Add(time.Minute))

	require.EqualValues(t, 1, runtimeHandler.deleteCalls.Load())
	require.False(t, s.allocations.HasAllocation(allocationID))
}
