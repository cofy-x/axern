package consistencykernel

import allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"

type RepairOwner string

const (
	RepairOwnerUnspecified         RepairOwner = ""
	RepairOwnerWorkloadController  RepairOwner = "workload_controller"
	RepairOwnerNodeLifecycle       RepairOwner = "node_lifecycle"
	RepairOwnerTunnelController    RepairOwner = "tunnel_controller"
	RepairOwnerServiceController   RepairOwner = "service_controller"
	RepairOwnerAdminOperatorTriage RepairOwner = "admin_operator_triage"
)

type RepairAction string

const (
	RepairActionUnspecified               RepairAction = ""
	RepairActionWorkloadCleanup           RepairAction = "workload_cleanup"
	RepairActionWorkloadCleanupAndReadmit RepairAction = "workload_cleanup_and_readmit"
	RepairActionNodeLifecycleReconcile    RepairAction = "node_lifecycle_reconcile"
	RepairActionTunnelLifecycleReconcile  RepairAction = "tunnel_lifecycle_reconcile"
	RepairActionServiceReconcile          RepairAction = "service_reconcile"
	RepairActionAdminTriage               RepairAction = "admin_triage"
)

type RepairTargetType string

const (
	RepairTargetTypeUnspecified   RepairTargetType = ""
	RepairTargetTypeAllocation    RepairTargetType = "allocation"
	RepairTargetTypeRun           RepairTargetType = "run"
	RepairTargetTypeService       RepairTargetType = "service"
	RepairTargetTypeTunnelSession RepairTargetType = "tunnel_session"
)

type RepairPlan struct {
	Owner      RepairOwner
	Action     RepairAction
	Automatic  bool
	TargetType RepairTargetType
	TargetID   string
}

func RepairPlanForIssue(issue Issue) RepairPlan {
	plan := repairPlanForCode(issue.Code)
	plan.TargetType, plan.TargetID = repairTargetForIssue(issue)
	return plan
}

func repairPlanForCode(code IssueCode) RepairPlan {
	switch code {
	case IssueActiveReservationMissingAllocation:
		return RepairPlan{
			Owner:  RepairOwnerAdminOperatorTriage,
			Action: RepairActionAdminTriage,
		}
	case IssueActiveReservationOnEndedAllocation:
		return RepairPlan{
			Owner:  RepairOwnerWorkloadController,
			Action: RepairActionWorkloadCleanup,
		}
	case IssueActiveReservationAllocationMismatch:
		return RepairPlan{
			Owner:  RepairOwnerWorkloadController,
			Action: RepairActionWorkloadCleanupAndReadmit,
		}
	case IssueActiveLeaseMissingAllocation, IssueActiveLeaseOnEndedAllocation, IssueActiveLeaseAllocationNodeMismatch:
		return RepairPlan{
			Owner:  RepairOwnerNodeLifecycle,
			Action: RepairActionNodeLifecycleReconcile,
		}
	case IssueActiveTunnelMissingAllocation, IssueActiveTunnelOnEndedAllocation, IssueActiveTunnelAllocationNodeMismatch:
		return RepairPlan{
			Owner:  RepairOwnerTunnelController,
			Action: RepairActionTunnelLifecycleReconcile,
		}
	case IssueServiceReferenceMissingAllocation, IssueServiceReferenceEndedAllocation, IssueServiceReferenceOwnerMismatch:
		return RepairPlan{
			Owner:  RepairOwnerServiceController,
			Action: RepairActionServiceReconcile,
		}
	default:
		return RepairPlan{}
	}
}

func repairTargetForIssue(issue Issue) (RepairTargetType, string) {
	switch issue.Code {
	case IssueActiveReservationMissingAllocation:
		return RepairTargetTypeAllocation, issue.AllocationID
	case IssueActiveReservationOnEndedAllocation, IssueActiveReservationAllocationMismatch:
		return workloadRepairTarget(issue)
	case IssueServiceReferenceMissingAllocation, IssueServiceReferenceEndedAllocation, IssueServiceReferenceOwnerMismatch:
		if issue.OwnerID != "" {
			return RepairTargetTypeService, issue.OwnerID
		}
		return allocationRepairTarget(issue)
	case IssueActiveTunnelMissingAllocation, IssueActiveTunnelOnEndedAllocation, IssueActiveTunnelAllocationNodeMismatch:
		if issue.DependentID != "" {
			return RepairTargetTypeTunnelSession, issue.DependentID
		}
		return allocationRepairTarget(issue)
	default:
		return allocationRepairTarget(issue)
	}
}

func workloadRepairTarget(issue Issue) (RepairTargetType, string) {
	switch issue.OwnerType {
	case string(allocationkernel.OwnerRun):
		if issue.OwnerID != "" {
			return RepairTargetTypeRun, issue.OwnerID
		}
	case string(allocationkernel.OwnerService):
		if issue.OwnerID != "" {
			return RepairTargetTypeService, issue.OwnerID
		}
	}
	return allocationRepairTarget(issue)
}

func allocationRepairTarget(issue Issue) (RepairTargetType, string) {
	if issue.AllocationID == "" {
		return RepairTargetTypeUnspecified, ""
	}
	return RepairTargetTypeAllocation, issue.AllocationID
}
