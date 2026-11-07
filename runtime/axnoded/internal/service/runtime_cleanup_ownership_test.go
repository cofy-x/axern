package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/require"
)

type cleanupProjectionRuntime struct {
	contract.SandboxRuntime
	manifest *apipb.AllocationEnforcementManifest
}

func (r cleanupProjectionRuntime) AllocationEnforcementManifest(context.Context, string) (*apipb.AllocationEnforcementManifest, error) {
	return r.manifest, nil
}

type cleanupOwnerVerifier struct {
	calls int
	err   error
}

func (v *cleanupOwnerVerifier) VerifyRuntimeConformanceBinding(string, *apipb.AllocationEnforcementManifest) error {
	v.calls++
	return v.err
}

func TestRuntimeCleanupRequiresTypedLeaseAndCheckpointForUnadmittedRuntime(t *testing.T) {
	for _, failure := range []string{"", "checkpoint", "bundle", "resource", "owner"} {
		t.Run(failure, func(t *testing.T) {
			const id = "arbitrary-runtime-id"
			manifest := &apipb.AllocationEnforcementManifest{CgroupPath: "/sandbox-test/" + id}
			h := newTestService(t, cleanupProjectionRuntime{SandboxRuntime: runtimetest.NewFakeSandboxRuntime(), manifest: manifest})
			h.config.RuntimeConfig.NativeRecovery = true
			manifest.BundlePath = filepath.Join(h.config.RootDir, "containers", id)
			if failure != "checkpoint" {
				require.NoError(t, h.containerManager.StoreMetadata(id, &apipb.ContainerMetadata{}))
			}
			if failure != "resource" {
				_, err := h.containerManager.Occupy(resourcemanager.AllocateOption{ContainerID: id}, resourcemanager.CgroupResourceName)
				require.NoError(t, err)
			}
			verifier := &cleanupOwnerVerifier{}
			h.conformanceRecovery = verifier
			if failure == "bundle" {
				manifest.BundlePath += "-wrong"
			}
			if failure == "owner" {
				verifier.err = errors.New("lease is owned by a workload, not conformance")
			}
			options, err := h.runtimeCleanupOptions(t.Context(), id)
			if failure == "" {
				require.NoError(t, err)
				require.True(t, options.RuntimeConformanceCleanup)
				require.Same(t, manifest, options.EnforcementManifest)
			} else if failure == "checkpoint" {
				require.NoError(t, err)
				require.Nil(t, options.EnforcementManifest)
				require.False(t, options.RuntimeConformanceCleanup)
			} else {
				require.Error(t, err)
				require.False(t, options.RuntimeConformanceCleanup)
			}
			if failure == "" || failure == "owner" {
				require.Equal(t, 1, verifier.calls)
			} else {
				require.Zero(t, verifier.calls)
			}
		})
	}
}

type cleanupSerializedRuntime struct {
	contract.SandboxRuntime
	calls int
}

func (r *cleanupSerializedRuntime) DeleteContainer(context.Context, *apipb.DeleteContainerRequest, contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
	r.calls++
	return &apipb.DeleteContainerResponse{}, nil
}
func TestNativeConformanceCleanupUsesExistingLifecycleLane(t *testing.T) {
	runtime := &cleanupSerializedRuntime{SandboxRuntime: runtimetest.NewFakeSandboxRuntime()}
	h := newTestService(t, runtime)
	h.config.RuntimeConfig.NativeRecovery = true
	unlock, err := h.allocationController().LockAllocationLifecycleContext(t.Context(), "probe")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	err = h.cleanupNativeConformanceRuntime(ctx, "probe")
	unlock()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, runtime.calls)
	require.NoError(t, h.cleanupNativeConformanceRuntime(t.Context(), "probe"))
	require.Equal(t, 1, runtime.calls)
}
