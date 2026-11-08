package workspace

import (
	"errors"

	lifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
)

func FromSpec(value *lifecyclev1.WorkspaceSpec) Spec {
	return Spec{ID: value.GetWorkspaceID(), Namespace: value.GetNamespace(), NodeID: value.GetNodeID(), Capacity: value.GetCapacityBytes(), Inodes: value.GetInodeLimit(), RootIdentity: value.GetStorageRootIdentity()}
}

func FromBinding(value *lifecyclev1.WorkspaceBinding) (Binding, error) {
	if value == nil || value.GetSpec() == nil || value.GetGeneration() <= 0 || value.GetStorageIdentity() == "" || value.GetTarget() != "/workspace" {
		return Binding{}, errors.New("Workspace requires its exact immutable binding at /workspace")
	}
	return Binding{Spec: FromSpec(value.GetSpec()), Identity: value.GetStorageIdentity(), Generation: uint64(value.GetGeneration())}, nil
}

func (r Receipt) Proto() *lifecyclev1.WorkspaceCleanupReceipt {
	return &lifecyclev1.WorkspaceCleanupReceipt{WorkspaceID: r.WorkspaceID, AllocationID: r.Allocation, Generation: int64(r.Generation), StorageIdentity: r.Identity}
}

func FromReceipt(value *lifecyclev1.WorkspaceCleanupReceipt) (Receipt, error) {
	if value.GetWorkspaceID() == "" || value.GetAllocationID() == "" || value.GetGeneration() <= 0 || value.GetStorageIdentity() == "" {
		return Receipt{}, errors.New("Workspace cleanup receipt is incomplete")
	}
	return Receipt{value.GetWorkspaceID(), value.GetAllocationID(), uint64(value.GetGeneration()), value.GetStorageIdentity()}, nil
}
