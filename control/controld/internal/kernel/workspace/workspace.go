package workspacekernel

import (
	"context"
	"time"

	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
)

const Target = "/workspace"

type Record struct {
	Workspace           *workspacev1.Workspace
	StorageRootIdentity string
	StorageIdentity     string
	NodeTarget          string
}

func (r *Record) Spec() *nodelifecyclev1.WorkspaceSpec {
	return &nodelifecyclev1.WorkspaceSpec{WorkspaceID: r.Workspace.GetID(), Namespace: r.Workspace.GetNamespace(), NodeID: r.Workspace.GetNodeID(), CapacityBytes: r.Workspace.GetCapacityBytes(), InodeLimit: r.Workspace.GetInodeLimit(), StorageRootIdentity: r.StorageRootIdentity}
}

func (r *Record) Binding() *nodelifecyclev1.WorkspaceBinding {
	return &nodelifecyclev1.WorkspaceBinding{Spec: r.Spec(), StorageIdentity: r.StorageIdentity, Generation: r.Workspace.GetGeneration(), Target: Target}
}

type ReconcileItem struct {
	Record     *Record
	ClaimOwner string
}

type Store interface {
	CreateWorkspace(context.Context, *workspacev1.CreateWorkspaceRequest, time.Time) (*workspacev1.Workspace, error)
	GetWorkspace(context.Context, string) (*workspacev1.Workspace, error)
	ListWorkspaces(context.Context, *workspacev1.ListWorkspacesRequest) ([]*workspacev1.Workspace, string, error)
	DeleteWorkspace(context.Context, string, time.Time) (*workspacev1.Workspace, error)
	ClaimWorkspaceReconcile(context.Context, string, time.Time) (*ReconcileItem, error)
	CompleteWorkspaceReconcile(context.Context, *ReconcileItem, string, int64, int64, error, time.Time) error
}

type NodeClient interface {
	ProvisionWorkspace(context.Context, string, *nodelifecyclev1.ProvisionWorkspaceRequest) (*nodelifecyclev1.ProvisionWorkspaceResponse, error)
	InspectWorkspace(context.Context, string, *nodelifecyclev1.InspectWorkspaceRequest) (*nodelifecyclev1.InspectWorkspaceResponse, error)
	DeleteWorkspace(context.Context, string, *nodelifecyclev1.DeleteWorkspaceRequest) (*nodelifecyclev1.DeleteWorkspaceResponse, error)
}
