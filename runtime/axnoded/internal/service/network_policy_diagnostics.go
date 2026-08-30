package service

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
)

func (h *sandboxService) NetworkPolicyDiagnostics(ctx context.Context, allocationID string) NetworkPolicyDiagnostics {
	diagnostics := NetworkPolicyDiagnostics{
		Mode:            NetworkPolicyModeUnrestricted,
		Status:          NetworkPolicyStatusAbsent,
		CapabilityState: NetworkPolicyCapabilityNotRequired,
	}
	dependencyMode := allocationNetworkPolicyMode(h.allocationController().CapabilityDependencies(allocationID))
	manifest, exists := h.allocationController().EgressPolicyManifest(allocationID)
	if !exists {
		if dependencyMode == NetworkPolicyModeUnrestricted {
			return diagnostics
		}
		diagnostics.Mode = dependencyMode
		if h.egressClient == nil {
			diagnostics.Status = NetworkPolicyStatusCapabilityUnavailable
			diagnostics.CapabilityState = NetworkPolicyCapabilityUnavailable
			return diagnostics
		}
		health, healthErr := h.egressClient.Health(ctx)
		if health != nil {
			diagnostics.EnforcementRevision = health.GetEnforcementRevision()
		}
		diagnostics.CapabilityState = h.networkPolicyCapabilityState(dependencyMode)
		diagnostics.EnforcementHealthy = networkPolicyEnforcementHealthy(health, dependencyMode)
		diagnostics.Status = networkPolicyDiagnosticStatus(diagnostics, healthErr, nil, false)
		return diagnostics
	}
	diagnostics.AllocationAttempt = manifest.Attempt
	diagnostics.ExecutionRevision = manifest.Proof.GetExecutionRevision()
	diagnostics.ExactProof = false
	diagnostics.Mode = dependencyMode

	if h.egressClient == nil {
		diagnostics.Status = NetworkPolicyStatusCapabilityUnavailable
		diagnostics.CapabilityState = NetworkPolicyCapabilityUnavailable
		return diagnostics
	}
	health, healthErr := h.egressClient.Health(ctx)
	if health != nil {
		diagnostics.EnforcementRevision = health.GetEnforcementRevision()
	}
	record, recordErr := h.egressClient.Get(ctx, allocationID, manifest.Attempt)
	if record != nil {
		applyNetworkPolicyRecord(&diagnostics, record, manifest, dependencyMode, allocationID)
	}
	diagnostics.CapabilityState = h.networkPolicyCapabilityState(diagnostics.Mode)
	diagnostics.EnforcementHealthy = networkPolicyEnforcementHealthy(health, diagnostics.Mode)

	diagnostics.Status = networkPolicyDiagnosticStatus(diagnostics, healthErr, recordErr, record != nil)
	return diagnostics
}

func applyNetworkPolicyRecord(diagnostics *NetworkPolicyDiagnostics, record *runtimeegressv1.PreparedEgressPolicy, manifest allocation.EgressPolicyManifest, dependencyMode NetworkPolicyMode, allocationID string) {
	recordMode, domainRules, cidrRules, portRanges := summarizeNetworkPolicy(record)
	diagnostics.Mode = recordMode
	diagnostics.DomainRuleCount = domainRules
	diagnostics.CIDRRuleCount = cidrRules
	diagnostics.PortRangeCount = portRanges
	diagnostics.TotalRuleCount = domainRules + cidrRules
	diagnostics.RecoveredAfterRestart = record.GetRecoveryState() == runtimeegressv1.EgressPolicyRecoveryState_EGRESS_POLICY_RECOVERY_STATE_RECOVERED
	diagnostics.ExactProof = record.GetAllocationID() == allocationID &&
		record.GetAttempt() == manifest.Attempt &&
		record.GetSandboxIp() == manifest.Proof.GetSandboxIp() &&
		record.GetPolicyDigest() == manifest.Proof.GetPolicyDigest() &&
		record.GetExecutionRevision() == manifest.Proof.GetExecutionRevision() &&
		dependencyMode == recordMode
}

func networkPolicyEnforcementHealthy(health *runtimeegressv1.EgressManagerHealth, mode NetworkPolicyMode) bool {
	if health == nil || health.GetStatus() != runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_OK {
		return false
	}
	switch mode {
	case NetworkPolicyModeDNSDeny:
		return health.GetDnsPolicySelfTestOk()
	case NetworkPolicyModeStrict:
		return health.GetStrictEgressSelfTestOk()
	default:
		return false
	}
}

func networkPolicyDiagnosticStatus(diagnostics NetworkPolicyDiagnostics, healthErr, recordErr error, recordPresent bool) NetworkPolicyStatus {
	switch {
	case diagnostics.CapabilityState != NetworkPolicyCapabilityAvailable:
		return NetworkPolicyStatusCapabilityUnavailable
	case healthErr != nil || !diagnostics.EnforcementHealthy:
		return NetworkPolicyStatusEnforcementUnhealthy
	case recordErr != nil || !recordPresent || !diagnostics.ExactProof:
		return NetworkPolicyStatusProofStale
	default:
		return NetworkPolicyStatusOK
	}
}

func allocationNetworkPolicyMode(dependencies []*capabilityv1.CapabilityDependency) NetworkPolicyMode {
	for _, dependency := range dependencies {
		switch dependency.GetKey().GetPlatform() {
		case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT:
			return NetworkPolicyModeStrict
		case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT:
			return NetworkPolicyModeDNSDeny
		}
	}
	return NetworkPolicyModeUnrestricted
}

func (h *sandboxService) networkPolicyCapabilityState(mode NetworkPolicyMode) NetworkPolicyCapabilityState {
	wanted := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_UNSPECIFIED
	switch mode {
	case NetworkPolicyModeDNSDeny:
		wanted = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT
	case NetworkPolicyModeStrict:
		wanted = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT
	default:
		return NetworkPolicyCapabilityUnknown
	}
	snapshot, ready := h.NodeInventory()
	if !ready || snapshot.Node.CapabilitySnapshot == nil {
		return NetworkPolicyCapabilityUnknown
	}
	for _, observation := range snapshot.Node.CapabilitySnapshot.GetObservations() {
		if observation.GetKey().GetPlatform() != wanted {
			continue
		}
		switch observation.GetState() {
		case capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE:
			return NetworkPolicyCapabilityAvailable
		case capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN,
			capabilityv1.CapabilityState_CAPABILITY_STATE_UNSPECIFIED:
			return NetworkPolicyCapabilityUnknown
		default:
			return NetworkPolicyCapabilityUnavailable
		}
	}
	return NetworkPolicyCapabilityUnknown
}

func summarizeNetworkPolicy(record *runtimeegressv1.PreparedEgressPolicy) (NetworkPolicyMode, uint32, uint32, uint32) {
	if strict := record.GetPolicy().GetStrict(); strict != nil {
		ports := 0
		for _, rule := range strict.GetAllowedCidrs() {
			ports += len(rule.GetPorts())
		}
		return NetworkPolicyModeStrict, uint32(len(strict.GetAllowedDomains())), uint32(len(strict.GetAllowedCidrs())), uint32(ports)
	}
	if dnsDeny := record.GetPolicy().GetDnsDeny(); dnsDeny != nil {
		return NetworkPolicyModeDNSDeny, uint32(len(dnsDeny.GetDeniedDomains())), 0, 0
	}
	return NetworkPolicyModeUnrestricted, 0, 0, 0
}
