package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	lifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodestate"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	"github.com/cofy-x/axern/runtime/axnoded/internal/workspace"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/require"
)

type withdrawnWorkspaceStorage struct{}

func (withdrawnWorkspaceStorage) Attach(workspace.Binding, string) (string, error) {
	panic("audit must never attach")
}
func (withdrawnWorkspaceStorage) CheckAttachment(workspace.Binding, string) error {
	panic("audit must never admit")
}
func (withdrawnWorkspaceStorage) VerifyAttachment(workspace.Binding, string) error {
	return errors.New("private quota limits changed")
}
func (withdrawnWorkspaceStorage) VerifyCleanupOwnership(workspace.Binding, string) error {
	panic("audit must not release")
}
func (withdrawnWorkspaceStorage) BeginCleanup(workspace.Binding, string) error {
	panic("audit must not release")
}
func (withdrawnWorkspaceStorage) Release(workspace.Binding, string) (workspace.Receipt, error) {
	panic("audit must not release")
}
func (withdrawnWorkspaceStorage) Receipt(workspace.Binding, string) (workspace.Receipt, bool, error) {
	panic("audit must not release")
}

func TestWorkspaceAuditFailStopsAndRetainsWriterDebt(t *testing.T) {
	handler := &deleteCountingRuntime{FakeSandboxRuntime: runtimetest.NewFakeSandboxRuntime()}
	s := newTestService(t, handler)
	options := s.allocationOptions()
	db, err := nodestate.Open(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	options.Store = db
	options.Workspaces = withdrawnWorkspaceStorage{}
	s.allocations = allocation.NewController(options)
	id := "allocation-workspace-audit"
	binding := &lifecyclev1.WorkspaceBinding{Spec: &lifecyclev1.WorkspaceSpec{WorkspaceID: "workspace-one", NodeID: "node-a", Namespace: "default", CapacityBytes: 8 << 20, InodeLimit: 128, StorageRootIdentity: "root-stable"}, Generation: 1, StorageIdentity: "data-stable", Target: "/workspace"}
	require.NoError(t, s.allocations.StoreAllocationIntent(id, "node-a", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", time.Now().Add(time.Minute), nil, nil, nil, nil, binding))
	require.NoError(t, s.allocations.StoreVerifiedEnforcementManifest(id, &apipb.AllocationEnforcementManifest{BundlePath: "/fake/" + id, CreatedAtUnixNano: time.Now().UnixNano()}, nil, time.Now()))
	require.NoError(t, s.containerManager.StoreMetadata(id, &apipb.ContainerMetadata{}))
	markTestContainerRunning(t, s, id)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.capabilityReconcileCtx = ctx
	require.True(t, s.auditWorkspaceAttachment(id))
	s.capabilityReconcileWG.Wait()
	code, _ := s.allocations.TerminationIntent(id)
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST, code)
	s.failStopAllocation(t.Context(), id, errors.New("Workspace enforcement unavailable"))
	require.EqualValues(t, 1, handler.stopCalls.Load())
	require.Zero(t, handler.deleteCalls.Load())
	require.True(t, s.allocations.HasAllocation(id))
	require.Equal(t, binding, s.allocations.WorkspaceBinding(id))
}
