package api

import (
	"context"
	"testing"

	lifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (f *fakeNodeLifecycleService) ProvisionWorkspace(context.Context, *lifecyclev1.ProvisionWorkspaceRequest) (*lifecyclev1.ProvisionWorkspaceResponse, error) {
	return &lifecyclev1.ProvisionWorkspaceResponse{StorageIdentity: "fixture-workspace"}, nil
}
func (f *fakeNodeLifecycleService) InspectWorkspace(context.Context, *lifecyclev1.InspectWorkspaceRequest) (*lifecyclev1.InspectWorkspaceResponse, error) {
	return &lifecyclev1.InspectWorkspaceResponse{}, nil
}
func (f *fakeNodeLifecycleService) DeleteWorkspace(context.Context, *lifecyclev1.DeleteWorkspaceRequest) (*lifecyclev1.DeleteWorkspaceResponse, error) {
	return &lifecyclev1.DeleteWorkspaceResponse{}, nil
}
func (f *fakeNodeLifecycleService) AcknowledgeWorkspaceCleanup(string, *lifecyclev1.WorkspaceCleanupReceipt) error {
	return nil
}

func TestWorkspaceLifecycleIsBoundAndControlPlaneOnly(t *testing.T) {
	spec := &lifecyclev1.WorkspaceSpec{WorkspaceID: "ws-one", Namespace: "default", NodeID: "node-one", CapacityBytes: 8 << 20, InodeLimit: 128, StorageRootIdentity: "root-one"}
	local := NewLocalNodeLifecycleServer(&fakeNodeLifecycleService{}, "node-one")
	_, err := local.ProvisionWorkspace(t.Context(), &lifecyclev1.ProvisionWorkspaceRequest{Spec: spec})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	remote := NewNodeLifecycleServer(&fakeNodeLifecycleService{}, "node-one")
	_, err = remote.ProvisionWorkspace(t.Context(), &lifecyclev1.ProvisionWorkspaceRequest{Spec: spec})
	require.NoError(t, err)
	spec.NodeID = "node-two"
	_, err = remote.ProvisionWorkspace(t.Context(), &lifecyclev1.ProvisionWorkspaceRequest{Spec: spec})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = remote.InspectWorkspace(t.Context(), &lifecyclev1.InspectWorkspaceRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
