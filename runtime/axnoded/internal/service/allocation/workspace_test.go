package allocation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	lifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/workspace"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/require"
)

type fakeWorkspaceStorage struct {
	root          string
	receipt       workspace.Receipt
	releaseCalls  int
	beforeRelease func()
}

func (f *fakeWorkspaceStorage) BeginCleanup(workspace.Binding, string) error           { return nil }
func (f *fakeWorkspaceStorage) VerifyCleanupOwnership(workspace.Binding, string) error { return nil }

func (f *fakeWorkspaceStorage) Attach(workspace.Binding, string) (string, error) { return f.root, nil }
func (f *fakeWorkspaceStorage) CheckAttachment(workspace.Binding, string) error  { return nil }
func (f *fakeWorkspaceStorage) VerifyAttachment(workspace.Binding, string) error { return nil }
func (f *fakeWorkspaceStorage) Receipt(binding workspace.Binding, id string) (workspace.Receipt, bool, error) {
	return f.receipt, f.receipt.Allocation == id, nil
}
func (f *fakeWorkspaceStorage) Release(binding workspace.Binding, id string) (workspace.Receipt, error) {
	if f.beforeRelease != nil {
		f.beforeRelease()
	}
	f.releaseCalls++
	f.receipt = workspace.Receipt{WorkspaceID: binding.Spec.ID, Allocation: id, Generation: binding.Generation, Identity: binding.Identity}
	return f.receipt, nil
}

func workspaceFixtureBinding() *lifecyclev1.WorkspaceBinding {
	return &lifecyclev1.WorkspaceBinding{Spec: &lifecyclev1.WorkspaceSpec{WorkspaceID: "workspace-one", Namespace: "default", NodeID: "node-a", CapacityBytes: 8 << 20, InodeLimit: 128, StorageRootIdentity: "root-stable"}, StorageIdentity: "data-stable", Generation: 1, Target: "/workspace"}
}

func TestWorkspaceMountFlowsThroughOrdinaryCreateAndExactCleanup(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, handler)
	storage := &fakeWorkspaceStorage{root: t.TempDir()}
	fixture.controller.workspaces = storage
	binding := workspaceFixtureBinding()
	id := "allocation-workspace"
	request := &apipb.StartRequest{AllocationID: id, Workspace: binding, Resources: &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 256 << 20}}, Environment: &apipb.ResolvedEnvironment{ID: "workspace-image", Rootfs: &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}}, Argv: []string{"/bin/sh", "-lc", "sleep 3600"}}}
	digest, err := StartRequestDigest(request)
	require.NoError(t, err)
	require.NoError(t, fixture.controller.StoreAllocationIntent(id, "node-a", digest, time.Now().Add(time.Minute), request.GetResources(), nil, nil, nil, binding))
	_, err = fixture.controller.Start(t.Context(), request)
	require.NoError(t, err)
	found := false
	for _, mount := range handler.lastRequest.GetMounts() {
		if mount.GetTarget() == "/workspace" {
			found = true
			require.Equal(t, storage.root, mount.GetSource())
		}
	}
	require.True(t, found, "managed bind must reach ordinary runtime preparation")
	require.Equal(t, binding, fixture.controller.WorkspaceBinding(id))
	storage.beforeRelease = func() {
		require.Equal(t, 1, handler.deleteCalls, "runtime deletion must precede writer receipt")
		_, err := os.Stat(filepath.Join(fixture.controller.config.RootDir, "containers", id))
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	response, err := fixture.controller.DeleteControlPlane(t.Context(), &apipb.DeleteRequest{ID: id, Workspace: binding}, "node-a")
	require.NoError(t, err)
	require.Equal(t, id, response.GetWorkspaceCleanup().GetAllocationID())
	require.Equal(t, int64(1), response.GetWorkspaceCleanup().GetGeneration())
	retry, err := fixture.controller.DeleteControlPlane(t.Context(), &apipb.DeleteRequest{ID: id, Workspace: binding}, "node-a")
	require.NoError(t, err)
	require.Equal(t, response.GetWorkspaceCleanup(), retry.GetWorkspaceCleanup())
	require.Equal(t, 1, storage.releaseCalls)
}

func TestWorkspaceWriterIsRetainedOnRuntimeDeleteFailure(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc", deleteErrors: []error{errors.New("runtime stop failed")}}
	fixture := newTestAllocationController(t, handler)
	storage := &fakeWorkspaceStorage{root: t.TempDir()}
	fixture.controller.workspaces = storage
	binding := workspaceFixtureBinding()
	id := "allocation-workspace-delete-error"
	require.NoError(t, fixture.controller.StoreAllocationIntent(id, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Now().Add(time.Minute), nil, nil, nil, nil, binding))
	storeTestContainer(t, fixture, id, "runsc")
	_, err := fixture.controller.DeleteControlPlane(t.Context(), &apipb.DeleteRequest{ID: id, Workspace: binding}, "node-a")
	require.Error(t, err)
	require.Zero(t, storage.releaseCalls)
	require.NotNil(t, fixture.controller.WorkspaceBinding(id))
}

func TestWorkspaceCleanupBeforeCreateRequiresCompleteRuntimeAbsence(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc", listStates: []*contract.UnionContainerState{{ID: "allocation-delayed"}}}
	fixture := newTestAllocationController(t, handler)
	storage := &fakeWorkspaceStorage{root: t.TempDir()}
	fixture.controller.workspaces = storage
	request := &apipb.DeleteRequest{ID: "allocation-delayed", Workspace: workspaceFixtureBinding()}
	_, err := fixture.controller.DeleteControlPlane(t.Context(), request, "node-a")
	require.Error(t, err)
	require.Zero(t, storage.releaseCalls)
	handler.listStates = nil
	response, err := fixture.controller.DeleteControlPlane(t.Context(), request, "node-a")
	require.NoError(t, err)
	require.Equal(t, int64(1), response.GetWorkspaceCleanup().GetGeneration())
	require.Equal(t, 1, storage.releaseCalls)
}
