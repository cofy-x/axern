package workspace

import (
	"context"
	"errors"
	"testing"

	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"google.golang.org/grpc"
)

type fixtureClient struct {
	create *workspacev1.CreateWorkspaceRequest
	value  *workspacev1.Workspace
}

func (f *fixtureClient) CreateWorkspace(_ context.Context, req *workspacev1.CreateWorkspaceRequest, _ ...grpc.CallOption) (*workspacev1.CreateWorkspaceResponse, error) {
	f.create = req
	return &workspacev1.CreateWorkspaceResponse{Workspace: f.value}, nil
}
func (f *fixtureClient) GetWorkspace(context.Context, *workspacev1.GetWorkspaceRequest, ...grpc.CallOption) (*workspacev1.GetWorkspaceResponse, error) {
	return &workspacev1.GetWorkspaceResponse{Workspace: f.value}, nil
}
func (f *fixtureClient) ListWorkspaces(context.Context, *workspacev1.ListWorkspacesRequest, ...grpc.CallOption) (*workspacev1.ListWorkspacesResponse, error) {
	return nil, nil
}
func (f *fixtureClient) DeleteWorkspace(context.Context, *workspacev1.DeleteWorkspaceRequest, ...grpc.CallOption) (*workspacev1.DeleteWorkspaceResponse, error) {
	return nil, nil
}

func TestCreatePreservesIndependentCapacityAndNodeIdentity(t *testing.T) {
	client := &fixtureClient{}
	_, err := New(client).Create(t.Context(), CreateParams{Namespace: "team", NodeID: "node-a", CapacityBytes: 1024 * 1024, InodeLimit: 64, Labels: map[string]string{"project": "example"}})
	if err != nil {
		t.Fatal(err)
	}
	if req := client.create; req.GetNamespace() != "team" || req.GetNodeID() != "node-a" || req.GetCapacityBytes() != 1024*1024 || req.GetInodeLimit() != 64 || req.GetLabels()["project"] != "example" {
		t.Fatalf("Workspace authority changed: %v", req)
	}
	client.create = nil
	if _, err := New(client).Create(t.Context(), CreateParams{Namespace: "team", NodeID: "node-a", CapacityBytes: 1024}); err == nil {
		t.Fatal("unbounded inode capacity accepted")
	}
	if client.create != nil {
		t.Fatal("invalid storage capacity reached server")
	}
}

func TestWaitReadyRejectsChangedIdentityAndUnavailableData(t *testing.T) {
	for _, value := range []*workspacev1.Workspace{nil, {ID: "different", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY}, {ID: "ws-a", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_UNAVAILABLE}} {
		if _, err := New(&fixtureClient{value: value}).WaitReady(t.Context(), "ws-a"); err == nil {
			t.Fatalf("invalid data readiness accepted: %v", value)
		}
	}
	client := &fixtureClient{value: &workspacev1.Workspace{ID: "ws-a", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_PROVISIONING}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := New(client).WaitReady(ctx, "ws-a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter ignored cancellation: %v", err)
	}
	client.value.Status = workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY
	if value, err := New(client).WaitReady(t.Context(), "ws-a"); err != nil || value.GetID() != "ws-a" {
		t.Fatalf("ready Workspace unavailable: %v, %v", value, err)
	}
}
