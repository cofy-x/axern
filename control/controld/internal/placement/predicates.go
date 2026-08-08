package placement

import (
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func containsRuntime(runtimes []string, runtimeName string) bool {
	for _, name := range runtimes {
		if name == runtimeName {
			return true
		}
	}
	return false
}

func imagemgrUsable(summary *nodev1.NodeSummary) bool {
	if summary == nil {
		return false
	}
	component := summary.GetComponents().GetImagemgr()
	return component.GetReachable() && component.GetState() != nodev1.ComponentState_COMPONENT_STATE_DISABLED
}

func imagefsdUsable(summary *nodev1.NodeSummary) bool {
	if summary == nil {
		return false
	}
	component := summary.GetComponents().GetImagefsd()
	return component.GetReachable() && component.GetState() != nodev1.ComponentState_COMPONENT_STATE_DISABLED
}

func effectiveNodeState(summary *nodev1.NodeSummary, heartbeatFresh bool) nodev1.NodeState {
	if !heartbeatFresh {
		return nodev1.NodeState_NODE_STATE_UNREACHABLE
	}
	if summary == nil {
		return nodev1.NodeState_NODE_STATE_UNSPECIFIED
	}
	switch summary.GetNodeState() {
	case nodev1.NodeState_NODE_STATE_DRAINING:
		return nodev1.NodeState_NODE_STATE_DRAINING
	case nodev1.NodeState_NODE_STATE_DISABLED:
		return nodev1.NodeState_NODE_STATE_DISABLED
	default:
		return nodev1.NodeState_NODE_STATE_READY
	}
}

func labelsMatch(labels, selector map[string]string) bool {
	if len(selector) == 0 {
		return true
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func hasCapability(summary *nodev1.NodeSummary, want string) bool {
	if want == "" {
		return true
	}
	for _, capability := range summary.GetCapabilities() {
		if capability == want {
			return true
		}
	}
	return false
}

func hasCapabilities(summary *nodev1.NodeSummary, wants []string) bool {
	for _, want := range wants {
		if !hasCapability(summary, want) {
			return false
		}
	}
	return true
}

func requiresPortsCapability(req *Request) bool {
	return req.GetRequiresHostPort() || len(req.GetPorts()) > 0
}

func requiredNetworkCapability(req *Request) string {
	if req == nil || req.GetNetwork() == "" {
		return ""
	}
	return "network:" + req.GetNetwork()
}

func hasAvailableCPU(policy resourcekernel.AdmissionPolicy, summary *nodev1.NodeSummary, requested int64) bool {
	if requested <= 0 {
		return true
	}
	used := summary.GetResources().GetAxnodedCommittedMilli()
	return policy.Fits(summary.GetAllocatable(), resourcekernel.Claim{CPUMilli: used}, resourcekernel.Claim{CPUMilli: requested})
}

func hasAvailableMemory(policy resourcekernel.AdmissionPolicy, summary *nodev1.NodeSummary, requested int64) bool {
	if requested <= 0 {
		return true
	}
	used := summary.GetResources().GetAxnodedCommittedBytes()
	return policy.Fits(summary.GetAllocatable(), resourcekernel.Claim{MemoryBytes: used}, resourcekernel.Claim{MemoryBytes: requested})
}

func hasAvailableEphemeralStorage(policy resourcekernel.AdmissionPolicy, summary *nodev1.NodeSummary, requested int64) bool {
	if requested <= 0 {
		return true
	}
	used := summary.GetResources().GetAxnodedEphemeralStorageCommittedBytes()
	return policy.Fits(summary.GetAllocatable(), resourcekernel.Claim{EphemeralStorageBytes: used}, resourcekernel.Claim{EphemeralStorageBytes: requested})
}
