package allocation

import (
	"context"
	"errors"
	"fmt"

	lifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/workspace"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

func (h *Controller) WorkspaceBinding(allocationID string) *lifecyclev1.WorkspaceBinding {
	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	var binding *lifecyclev1.WorkspaceBinding
	if state != nil && state.record.GetWorkspace() != nil {
		binding = proto.Clone(state.record.GetWorkspace()).(*lifecyclev1.WorkspaceBinding)
	}
	h.stateMu.RUnlock()
	return binding
}

func (h *Controller) VerifyWorkspaceAttachment(allocationID string) error {
	value := h.WorkspaceBinding(allocationID)
	if value == nil {
		return nil
	}
	return h.verifyWorkspaceBinding(value, allocationID)
}

func (h *Controller) CheckWorkspaceAdmission(request *apipb.StartRequest) error {
	if request.GetWorkspace() == nil {
		return nil
	}
	if h.workspaces == nil {
		return errors.New("Workspace manager unavailable")
	}
	binding, err := workspace.FromBinding(request.GetWorkspace())
	if err != nil {
		return err
	}
	return h.workspaces.CheckAttachment(binding, request.GetAllocationID())
}

func (h *Controller) verifyWorkspaceBinding(value *lifecyclev1.WorkspaceBinding, allocationID string) error {
	if h.workspaces == nil {
		return errors.New("Workspace storage manager is unavailable")
	}
	binding, err := workspace.FromBinding(value)
	if err != nil {
		return err
	}
	return h.workspaces.VerifyAttachment(binding, allocationID)
}

func (h *Controller) workspaceMount(request *apipb.StartRequest) (*apipb.Mount, error) {
	if request.GetWorkspace() == nil {
		return nil, nil
	}
	if h.workspaces == nil {
		return nil, fmt.Errorf("Workspace storage is unavailable: %w", errord.ErrFailedPrecondition)
	}
	binding, err := workspace.FromBinding(request.GetWorkspace())
	if err != nil {
		return nil, err
	}
	path, err := h.workspaces.Attach(binding, request.GetAllocationID())
	if err != nil {
		return nil, fmt.Errorf("Workspace attachment unavailable: %w", errord.ErrFailedPrecondition)
	}
	return &apipb.Mount{Type: "bind", Source: path, Target: "/workspace", Options: []string{"bind", "rw", "nosuid", "nodev"}}, nil
}

func (h *Controller) verifyCleanupWorkspace(request *apipb.DeleteRequest) error {
	current := h.WorkspaceBinding(request.GetID())
	if request.GetWorkspace() == nil {
		request.Workspace = current
		return nil
	}
	if current != nil && !proto.Equal(current, request.GetWorkspace()) {
		return fmt.Errorf("Workspace cleanup conflicts with admitted binding: %w", errord.ErrFailedPrecondition)
	}
	_, err := workspace.FromBinding(request.GetWorkspace())
	return err
}

func (h *Controller) workspaceCleanupReceipt(request *apipb.DeleteRequest) (*lifecyclev1.WorkspaceCleanupReceipt, bool, error) {
	if request.GetWorkspace() == nil {
		return nil, false, nil
	}
	if h.workspaces == nil {
		return nil, false, errors.New("Workspace manager is unavailable")
	}
	binding, err := workspace.FromBinding(request.GetWorkspace())
	if err != nil {
		return nil, false, err
	}
	receipt, found, err := h.workspaces.Receipt(binding, request.GetID())
	if err != nil || !found {
		return nil, found, err
	}
	return receipt.Proto(), true, nil
}

func (h *Controller) releaseWorkspace(request *apipb.DeleteRequest) (*lifecyclev1.WorkspaceCleanupReceipt, error) {
	if request.GetWorkspace() == nil {
		return nil, nil
	}
	if h.workspaces == nil {
		return nil, errors.New("Workspace manager is unavailable")
	}
	binding, err := workspace.FromBinding(request.GetWorkspace())
	if err != nil {
		return nil, err
	}
	receipt, err := h.workspaces.Release(binding, request.GetID())
	if err != nil {
		return nil, err
	}
	return receipt.Proto(), nil
}

// A missing container-manager entry is not evidence of runtime absence.
// Workspace writer release requires the complete runsc inventory as well.
func (h *Controller) verifyWorkspaceRuntimeAbsence(ctx context.Context, allocationID string) error {
	if h.runscHandler == nil {
		return errors.New("cannot prove Workspace runtime absence")
	}
	all, err := h.runscHandler.ListContainers(ctx, contract.HandlerOptions{})
	if err != nil {
		return err
	}
	for _, runtime := range all {
		if runtime != nil && runtime.ID == allocationID {
			return fmt.Errorf("Workspace runtime remains present: %w", errord.ErrFailedPrecondition)
		}
	}
	return nil
}

// Restore validates the retained fence before acquiring image ownership or
// permitting a recovered process to resume.
func (h *Controller) restoreWorkspace(record *apipb.AllocationState) error {
	if record.GetWorkspace() == nil {
		return nil
	}
	if record.GetNodeID() == "" || record.GetWorkspace().GetSpec().GetNodeID() != record.GetNodeID() {
		return errors.New("Workspace recovery node identity mismatch")
	}
	if target, err := h.containers().Get(record.GetAllocationID()); err == nil && target.Status != nil && target.Status.Get().State() == apipb.ContainerState_CONTAINER_EXITED {
		binding, err := workspace.FromBinding(record.GetWorkspace())
		if err != nil {
			return err
		}
		if h.workspaces == nil {
			return errors.New("Workspace manager unavailable")
		}
		return h.workspaces.VerifyCleanupOwnership(binding, record.GetAllocationID())
	}
	return h.verifyWorkspaceBinding(record.GetWorkspace(), record.GetAllocationID())
}

func (h *Controller) beginWorkspaceCleanup(request *apipb.DeleteRequest) error {
	if request.GetWorkspace() == nil {
		return nil
	}
	if h.workspaces == nil {
		return errors.New("Workspace manager unavailable")
	}
	if h.HasAllocation(request.GetID()) {
		if err := h.MarkTerminationIntentWithLifecycleHeld(request.GetID(), commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_ALLOCATION_CLEANUP_REQUESTED, "allocation cleanup requested"); err != nil {
			return err
		}
	}
	binding, err := workspace.FromBinding(request.GetWorkspace())
	if err != nil {
		return err
	}
	return h.workspaces.BeginCleanup(binding, request.GetID())
}

func (h *Controller) PersistedWorkspaceBinding(allocationID string) (*lifecyclev1.WorkspaceBinding, error) {
	if current := h.WorkspaceBinding(allocationID); current != nil {
		return current, nil
	}
	if h.store == nil {
		return nil, nil
	}
	var record apipb.AllocationState
	if err := h.store.GetRecord(config.AllocationStateBucket, allocationID, &record); err != nil {
		if errors.Is(err, errord.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return record.GetWorkspace(), nil
}

func (h *Controller) WorkspacePending(allocationID string) (bool, error) {
	binding, err := h.PersistedWorkspaceBinding(allocationID)
	return binding != nil, err
}

func (h *Controller) cleanupAbsentWorkspaceRuntime(ctx context.Context, allocationID string) error {
	_, err := h.runscHandler.DeleteContainer(ctx, &apipb.DeleteContainerRequest{ID: allocationID, Timeout: 0}, contract.HandlerOptions{ContainerID: allocationID, ForceDelete: true, EnforcementManifest: h.EnforcementManifest(allocationID)})
	if err != nil && !isDeleteNotFound(err) {
		return fmt.Errorf("cleanup absent Workspace runtime artifacts: %w", err)
	}
	return nil
}
