package consistencykernel

type RepairOwner string

const (
	RepairOwnerUnspecified      RepairOwner = ""
	RepairOwnerRunController    RepairOwner = "run_controller"
	RepairOwnerNodeLifecycle    RepairOwner = "node_lifecycle"
	RepairOwnerTunnelController RepairOwner = "tunnel_controller"
)

type RepairAction string

const (
	RepairActionUnspecified              RepairAction = ""
	RepairActionRunCleanup               RepairAction = "run_cleanup"
	RepairActionNodeLifecycleReconcile   RepairAction = "node_lifecycle_reconcile"
	RepairActionTunnelLifecycleReconcile RepairAction = "tunnel_lifecycle_reconcile"
)

type RepairTargetType string

const (
	RepairTargetTypeUnspecified   RepairTargetType = ""
	RepairTargetTypeAllocation    RepairTargetType = "allocation"
	RepairTargetTypeRun           RepairTargetType = "run"
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
	case IssueActiveAccessGrantOnEndedAllocation:
		return RepairPlan{
			Owner:  RepairOwnerNodeLifecycle,
			Action: RepairActionNodeLifecycleReconcile,
		}
	case IssueActiveTunnelOnEndedAllocation:
		return RepairPlan{
			Owner:  RepairOwnerTunnelController,
			Action: RepairActionTunnelLifecycleReconcile,
		}
	default:
		return RepairPlan{}
	}
}

func repairTargetForIssue(issue Issue) (RepairTargetType, string) {
	switch issue.Code {
	case IssueActiveTunnelOnEndedAllocation:
		if issue.DependentID != "" {
			return RepairTargetTypeTunnelSession, issue.DependentID
		}
		return allocationRepairTarget(issue)
	default:
		return allocationRepairTarget(issue)
	}
}

func allocationRepairTarget(issue Issue) (RepairTargetType, string) {
	if issue.AllocationID == "" {
		return RepairTargetTypeUnspecified, ""
	}
	return RepairTargetTypeAllocation, issue.AllocationID
}
