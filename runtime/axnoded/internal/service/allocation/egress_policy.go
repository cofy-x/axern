package allocation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

func (h *Controller) prepareEgressPolicy(ctx context.Context, request *runtime.StartRequest, resource container.OccupiedResource) (bool, error) {
	if request == nil || request.GetEgressPolicy() == nil {
		return false, nil
	}
	if h.egress == nil {
		return false, fmt.Errorf("egressd is required for a sandbox network policy")
	}
	if request.GetAllocationAttempt() <= 0 {
		return false, fmt.Errorf("a positive allocation attempt is required for a sandbox network policy")
	}
	sandboxIP := strings.TrimSpace(containerIPFromResource(resource))
	if sandboxIP == "" {
		return false, fmt.Errorf("sandbox network policy requires an allocated interface IP")
	}
	revision := int64(1)
	if conditions := h.CapabilityConditions(request.GetContainerID()); conditions != nil && conditions.GetRevision() > 0 {
		revision = conditions.GetRevision()
	}
	dnsConfig := h.config.PluginConfig.RuntimeConfig.DNS
	upstreams, err := runtimeoci.ResolveRuntimeDNSNameservers(runtimeoci.RuntimeDNSConfig{
		Nameservers: append([]string(nil), dnsConfig.Nameservers...), SearchDomains: append([]string(nil), dnsConfig.SearchDomains...), Options: append([]string(nil), dnsConfig.Options...),
	})
	if err != nil {
		return false, fmt.Errorf("resolve trusted egress DNS upstreams: %w", err)
	}
	prepared, err := h.egress.Prepare(ctx, request.GetContainerID(), request.GetAllocationAttempt(), sandboxIP, request.GetEgressPolicy(), revision, upstreams)
	if err != nil {
		// The RPC may have crossed the dataplane boundary before transport or
		// persistence failure became visible. Treat ownership as uncertain and
		// require an attempt-fenced Delete before releasing this source IP.
		return true, fmt.Errorf("prepare egress policy: %w", err)
	}
	if prepared == nil || prepared.GetAllocationID() != request.GetContainerID() || prepared.GetAttempt() != request.GetAllocationAttempt() || prepared.GetSandboxIp() != sandboxIP || prepared.GetExecutionRevision() != revision {
		return true, fmt.Errorf("egressd returned an invalid exact policy proof")
	}
	if err := h.StoreEgressPolicyProof(request.GetContainerID(), sandboxIP, prepared.GetPolicyDigest(), revision); err != nil {
		if deleteErr := h.egress.Delete(context.Background(), request.GetContainerID(), request.GetAllocationAttempt()); deleteErr != nil {
			return true, errors.Join(err, fmt.Errorf("rollback unpersisted egress policy: %w", deleteErr))
		}
		return false, err
	}
	return true, nil
}

func (h *Controller) allocationHasEgressPolicy(allocationID string) bool {
	for _, dependency := range h.CapabilityDependencies(allocationID) {
		switch dependency.GetKey().GetPlatform() {
		case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT:
			return true
		}
	}
	return false
}

func (h *Controller) deleteEgressPolicy(ctx context.Context, allocationID string, attempt int64) error {
	if h.egress == nil || attempt <= 0 {
		return nil
	}
	if err := h.egress.Delete(ctx, allocationID, attempt); err != nil {
		return fmt.Errorf("delete egress policy: %w", err)
	}
	return nil
}
