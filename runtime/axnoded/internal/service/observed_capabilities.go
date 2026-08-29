package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	nodecapabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
)

type observedProvider struct {
	provider capabilityv1.CapabilityProvider
	expected []*capabilityv1.CapabilityKey
	observe  func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error)
}

func (p observedProvider) Provider() capabilityv1.CapabilityProvider { return p.provider }
func (p observedProvider) ExpectedKeys() []*capabilityv1.CapabilityKey {
	out := make([]*capabilityv1.CapabilityKey, 0, len(p.expected))
	for _, key := range p.expected {
		out = append(out, capabilitycontract.CloneKey(key))
	}
	return out
}
func (p observedProvider) Observe(ctx context.Context, now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
	return p.observe(ctx, now)
}

type derivedCapabilityProvider struct {
	expected []*capabilityv1.CapabilityKey
}

func (p derivedCapabilityProvider) Provider() capabilityv1.CapabilityProvider {
	return capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED
}
func (p derivedCapabilityProvider) ExpectedKeys() []*capabilityv1.CapabilityKey {
	return cloneCapabilityKeys(p.expected)
}
func (p derivedCapabilityProvider) Observe(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
	return nil, nil
}
func (p derivedCapabilityProvider) Derive(_ context.Context, _ time.Time, base map[string]*capabilityv1.CapabilityObservation) ([]*capabilityv1.CapabilityObservation, error) {
	result := make([]*capabilityv1.CapabilityObservation, 0, len(p.expected))
	for _, key := range p.expected {
		dependencyKeys, err := capabilitycontract.PlatformDependencyKeys(key.GetPlatform())
		if err != nil {
			return nil, err
		}
		available := true
		for _, dependencyKey := range dependencyKeys {
			id, err := capabilitycontract.KeyID(dependencyKey)
			if err != nil {
				return nil, err
			}
			if base[id] == nil || base[id].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
				available = false
				break
			}
		}
		if available {
			result = append(result, availableObservation(key, nil))
			continue
		}
		result = append(result, failedObservation(key, nil, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE, "one or more capability dependencies are unavailable"))
	}
	return result, nil
}

func (h *sandboxService) newObservedCapabilityManager(cgroupRoot string) (*nodecapabilitymanager.Manager, error) {
	cfg := h.config
	networkConfig, err := cfg.PluginConfig.NetworkConfig.Normalized()
	if err != nil {
		return nil, fmt.Errorf("normalize network capability config: %w", err)
	}
	cfg.PluginConfig.NetworkConfig = networkConfig
	networkDigest, err := networkConfigDigest(networkConfig)
	if err != nil {
		return nil, err
	}
	extensions, err := cfg.PluginConfig.NodeExtensionCapabilitiesValue()
	if err != nil {
		return nil, fmt.Errorf("node extension capabilities: %w", err)
	}
	extensionRequirements := make([]*capabilityv1.ExtensionCapabilityRequirement, 0, len(extensions))
	for _, extension := range extensions {
		extensionRequirements = append(extensionRequirements, &capabilityv1.ExtensionCapabilityRequirement{Capability: extension})
	}
	if err := capabilitycontract.ValidateExtensionRequirements(extensionRequirements); err != nil {
		return nil, fmt.Errorf("node extension capabilities: %w", err)
	}
	bootID, bootErr := hostlinux.CurrentBootID()
	runtimeDigestCache := newRuntimeFileDigestCache()
	providers := make([]nodecapabilitymanager.Provider, 0, 9)
	if len(extensions) > 0 {
		providers = append(providers, configCapabilityProvider(extensions, extensionConfigDigest(extensions)))
	}
	providers = append(providers,
		networkCapabilityProvider(cfg, networkDigest),
		cgroupCapabilityProvider(cfg, cgroupRoot, bootID, bootErr),
		filestoreCapabilityProvider(cfg, bootID, bootErr),
		runtimeConformanceCapabilityProvider(cfg, h.runtimeHandlers, config.RuntimeNameRunc, runtimeConformanceKindMemory, bootID, h.runRuntimeConformanceSelfTest, runtimeDigestCache),
		runtimeConformanceCapabilityProvider(cfg, h.runtimeHandlers, config.RuntimeNameRunc, runtimeConformanceKindEphemeral, bootID, h.runRuntimeConformanceSelfTest, runtimeDigestCache),
		runtimeConformanceCapabilityProvider(cfg, h.runtimeHandlers, config.RuntimeNameRunsc, runtimeConformanceKindMemory, bootID, h.runRuntimeConformanceSelfTest, runtimeDigestCache),
		runtimeConformanceCapabilityProvider(cfg, h.runtimeHandlers, config.RuntimeNameRunsc, runtimeConformanceKindEphemeral, bootID, h.runRuntimeConformanceSelfTest, runtimeDigestCache),
		derivedCapabilityProvider{expected: []*capabilityv1.CapabilityKey{
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT),
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT),
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT),
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT),
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT),
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT),
		}},
	)
	if err := nodecapabilitymanager.ValidateCatalogProviderCoverage(providers...); err != nil {
		return nil, fmt.Errorf("validate capability provider catalog coverage: %w", err)
	}
	return nodecapabilitymanager.NewManager(providers...)
}

func configCapabilityProvider(extensions []*capabilityv1.ExtensionCapability, digest string) nodecapabilitymanager.Provider {
	expected := make([]*capabilityv1.CapabilityKey, 0, len(extensions))
	for _, extension := range extensions {
		expected = append(expected, capabilitycontract.ExtensionKey(extension.GetName(), extension.GetValue()))
	}
	return observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG,
		expected: expected,
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			out := make([]*capabilityv1.CapabilityObservation, 0, len(expected))
			for _, key := range expected {
				out = append(out, availableObservation(key, capabilitycontract.ConfigEvidence(digest)))
			}
			return out, nil
		},
	}
}

func networkCapabilityProvider(cfg config.Config, digest string) nodecapabilitymanager.Provider {
	keys := []*capabilityv1.CapabilityKey{
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING),
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE),
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET),
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_DNS_POLICY_SELF_TEST),
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_STRICT_EGRESS_SELF_TEST),
	}
	return observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		expected: keys,
		observe: func(_ context.Context, _ time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			manager := networkmanager.NetworkManagers[cfg.PluginConfig.NetworkConfig.NatBackend]
			probe, ok := manager.(networkmanager.HealthProber)
			evidence := capabilitycontract.ConfigEvidence(digest)
			activeIndex := 1
			inactiveIndex := 2
			if cfg.PluginConfig.NetworkConfig.NatBackend == config.NatBackendEBPF {
				activeIndex, inactiveIndex = 2, 1
			}
			observations := make([]*capabilityv1.CapabilityObservation, len(keys))
			observations[3] = failedObservation(keys[3], evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DISABLED, "egressd DNS policy enforcement is not configured")
			observations[4] = failedObservation(keys[4], evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DISABLED, "egressd strict egress enforcement is not configured")
			observations[inactiveIndex] = failedObservation(keys[inactiveIndex], evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DISABLED, "network backend is not selected by node configuration")
			if !ok {
				observations[0] = failedObservation(keys[0], evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, "network backend has no operational health probe")
				observations[activeIndex] = failedObservation(keys[activeIndex], evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, "network backend has no operational health probe")
				return observations, nil
			}
			health, err := probe.ProbeHealth(cfg.PluginConfig.NetworkConfig.IPRange)
			if err != nil {
				observations[0] = unknownCapabilityObservation(keys[0], evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, err.Error())
				observations[activeIndex] = unknownCapabilityObservation(keys[activeIndex], evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, err.Error())
				return observations, nil
			}
			observations[0] = boolObservation(keys[0], health.PortForwardingReady, evidence, "port-forwarding dataplane is unavailable")
			observations[activeIndex] = boolObservation(keys[activeIndex], health.NativeDataplaneReady, evidence, "network dataplane is unavailable")
			return observations, nil
		},
	}
}

func cgroupCapabilityProvider(cfg config.Config, rootName, bootID string, bootErr error) nodecapabilitymanager.Provider {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER)
	return observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_HOST_CGROUP,
		expected: []*capabilityv1.CapabilityKey{key},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			if bootErr != nil {
				return []*capabilityv1.CapabilityObservation{unknownCapabilityObservation(key, nil, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, bootErr.Error())}, nil
			}
			mode, err := cfg.PluginConfig.RuntimeConfig.CgroupEnforcementMode()
			if err != nil {
				return nil, err
			}
			evidence := capabilitycontract.BootEvidence(bootID)
			if mode != config.CgroupEnforcementRequired {
				return []*capabilityv1.CapabilityObservation{failedObservation(key, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DISABLED, "cgroup enforcement is disabled for development")}, nil
			}
			if err := hostlinux.ProbeCgroupMemoryLimit(rootName); err != nil {
				return []*capabilityv1.CapabilityObservation{failedObservation(key, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, err.Error())}, nil
			}
			return []*capabilityv1.CapabilityObservation{availableObservation(key, evidence)}, nil
		},
	}
}

// filestoreCapabilityProvider reads one mount-scoped artifact and publishes
// OverlayFS, project-quota, and EROFS observations as one atomic batch.
func filestoreCapabilityProvider(cfg config.Config, bootID string, bootErr error) nodecapabilitymanager.Provider {
	keys := []*capabilityv1.CapabilityKey{
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER),
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA),
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS),
	}
	return observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE,
		expected: keys,
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			if bootErr != nil {
				return unknownCapabilityObservations(keys, nil, bootErr.Error()), nil
			}
			filestore := strings.TrimSpace(cfg.PluginConfig.RuntimeConfig.FilestoreDir)
			if filestore == "" {
				return failedObservations(keys, nil, "runtime filestore is not configured"), nil
			}
			facts, err := hostlinux.ReadFilestoreCapabilities(filestore)
			if err != nil {
				return unknownCapabilityObservations(keys, nil, err.Error()), nil
			}
			evidence := capabilitycontract.MountEvidence(bootID, facts.MountIdentity)
			return []*capabilityv1.CapabilityObservation{
				boolObservation(keys[0], facts.OverlayReady, evidence, "OverlayFS upper probe failed"),
				boolObservation(keys[1], facts.ProjectQuotaReady, evidence, "XFS project quota probe failed"),
				boolObservation(keys[2], facts.EROFSReady, evidence, firstNonEmptyCapabilityReason(facts.EROFSProbeError, "EROFS lower probe failed")),
			}, nil
		},
	}
}

func availableObservation(key *capabilityv1.CapabilityKey, evidence *capabilityv1.CapabilityEvidence) *capabilityv1.CapabilityObservation {
	return &capabilityv1.CapabilityObservation{Key: capabilitycontract.CloneKey(key), State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, Evidence: cloneEvidence(evidence), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE}
}

func failedObservation(key *capabilityv1.CapabilityKey, evidence *capabilityv1.CapabilityEvidence, code capabilityv1.CapabilityReasonCode, reason string) *capabilityv1.CapabilityObservation {
	return &capabilityv1.CapabilityObservation{Key: capabilitycontract.CloneKey(key), State: capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE, Evidence: cloneEvidence(evidence), ReasonCode: code, Reason: capabilitycontract.BoundedReason(reason)}
}

func unknownCapabilityObservation(key *capabilityv1.CapabilityKey, evidence *capabilityv1.CapabilityEvidence, code capabilityv1.CapabilityReasonCode, reason string) *capabilityv1.CapabilityObservation {
	observation := failedObservation(key, evidence, code, reason)
	observation.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN
	return observation
}

func unknownCapabilityObservations(keys []*capabilityv1.CapabilityKey, evidence *capabilityv1.CapabilityEvidence, reason string) []*capabilityv1.CapabilityObservation {
	out := make([]*capabilityv1.CapabilityObservation, 0, len(keys))
	for _, key := range keys {
		out = append(out, unknownCapabilityObservation(key, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, reason))
	}
	return out
}

func boolObservation(key *capabilityv1.CapabilityKey, available bool, evidence *capabilityv1.CapabilityEvidence, reason string) *capabilityv1.CapabilityObservation {
	if available {
		return availableObservation(key, evidence)
	}
	return failedObservation(key, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, reason)
}

func failedObservations(keys []*capabilityv1.CapabilityKey, evidence *capabilityv1.CapabilityEvidence, reason string) []*capabilityv1.CapabilityObservation {
	out := make([]*capabilityv1.CapabilityObservation, 0, len(keys))
	for _, key := range keys {
		out = append(out, failedObservation(key, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, reason))
	}
	return out
}

func cloneEvidence(evidence *capabilityv1.CapabilityEvidence) *capabilityv1.CapabilityEvidence {
	if evidence == nil {
		return nil
	}
	return proto.Clone(evidence).(*capabilityv1.CapabilityEvidence)
}

func cloneCapabilityKeys(keys []*capabilityv1.CapabilityKey) []*capabilityv1.CapabilityKey {
	out := make([]*capabilityv1.CapabilityKey, 0, len(keys))
	for _, key := range keys {
		out = append(out, capabilitycontract.CloneKey(key))
	}
	return out
}

func networkConfigDigest(network config.NetworkConfig) (string, error) {
	normalized, err := network.Normalized()
	if err != nil {
		return "", fmt.Errorf("normalize network evidence config: %w", err)
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal network evidence config: %w", err)
	}
	return sha256Digest(payload), nil
}

func extensionConfigDigest(extensions []*capabilityv1.ExtensionCapability) string {
	normalized := make([]*capabilityv1.ExtensionCapability, 0, len(extensions))
	for _, extension := range extensions {
		normalized = append(normalized, capabilitycontract.NormalizeExtension(extension))
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].GetName() == normalized[j].GetName() {
			return normalized[i].GetValue() < normalized[j].GetValue()
		}
		return normalized[i].GetName() < normalized[j].GetName()
	})
	payload, _ := json.Marshal(normalized)
	return sha256Digest(payload)
}

func sha256Digest(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func firstNonEmptyCapabilityReason(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "capability probe failed"
}
