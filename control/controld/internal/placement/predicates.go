package placement

import (
	"time"

	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
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

func hasCapability(summary *nodev1.NodeSummary, want *capabilityv1.CapabilityKey, now time.Time) bool {
	_, available := capabilitycontract.AvailableObservation(summary.GetCapabilitySnapshot(), want, now)
	return available
}

func hasCapabilities(summary *nodev1.NodeSummary, wants []*capabilityv1.CapabilityKey, now time.Time) bool {
	for _, want := range wants {
		if !hasCapability(summary, want, now) {
			return false
		}
	}
	return true
}

func requiresPortsCapability(req *placementkernel.Request) bool {
	return req.GetRequiresHostPort() || len(req.GetPorts()) > 0
}

func requiresNodeDataplane(req *placementkernel.Request) bool {
	return req != nil && req.GetNetwork() != "host"
}

func availableNetworkCapability(summary *nodev1.NodeSummary, now time.Time) *capabilityv1.CapabilityKey {
	for _, platform := range []capabilityv1.PlatformCapability{
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
	} {
		key := capabilitycontract.PlatformKey(platform)
		if hasCapability(summary, key, now) {
			return key
		}
	}
	return nil
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
