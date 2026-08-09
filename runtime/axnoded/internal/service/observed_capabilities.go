package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	nodecapabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type observedProvider struct {
	provider capabilityv1.CapabilityProvider
	expected []*capabilityv1.CapabilityKey
	observe  func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error)
}

type startupProvider struct {
	delegate     nodecapabilitymanager.Provider
	once         sync.Once
	observations []*capabilityv1.CapabilityObservation
	err          error
}

func (p *startupProvider) Provider() capabilityv1.CapabilityProvider { return p.delegate.Provider() }
func (p *startupProvider) ExpectedKeys() []*capabilityv1.CapabilityKey {
	return p.delegate.ExpectedKeys()
}
func (p *startupProvider) Observe(ctx context.Context, now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
	p.once.Do(func() {
		p.observations, p.err = p.delegate.Observe(ctx, now)
	})
	out := make([]*capabilityv1.CapabilityObservation, 0, len(p.observations))
	for _, observation := range p.observations {
		if observation != nil {
			out = append(out, proto.Clone(observation).(*capabilityv1.CapabilityObservation))
		}
	}
	return out, p.err
}

func (p observedProvider) Provider() capabilityv1.CapabilityProvider { return p.provider }
func (p observedProvider) ExpectedKeys() []*capabilityv1.CapabilityKey {
	out := make([]*capabilityv1.CapabilityKey, 0, len(p.expected))
	for _, key := range p.expected {
		out = append(out, proto.Clone(key).(*capabilityv1.CapabilityKey))
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
func (p derivedCapabilityProvider) ExpectedKeys() []*capabilityv1.CapabilityKey { return p.expected }
func (p derivedCapabilityProvider) Observe(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
	return nil, nil
}
func (p derivedCapabilityProvider) Derive(_ context.Context, now time.Time, base map[string]*capabilityv1.CapabilityObservation) ([]*capabilityv1.CapabilityObservation, error) {
	result := make([]*capabilityv1.CapabilityObservation, 0, len(p.expected))
	for _, key := range p.expected {
		dependencyKeys, err := capabilitycontract.PlatformDependencyKeys(key.GetPlatform())
		if err != nil {
			return nil, err
		}
		dependencies := make([]*capabilityv1.CapabilityObservation, 0, len(dependencyKeys))
		for _, dependencyKey := range dependencyKeys {
			id, err := capabilitycontract.KeyID(dependencyKey)
			if err != nil {
				return nil, err
			}
			dependencies = append(dependencies, base[id])
		}
		result = append(result, derivedObservation(key.GetPlatform(), now, dependencies...))
	}
	return result, nil
}

func (h *sandboxService) newObservedCapabilityManager(cgroupRoot string) (*nodecapabilitymanager.Manager, error) {
	cfg := h.config
	extensions, err := cfg.PluginConfig.NodeExtensionCapabilitiesValue()
	if err != nil {
		return nil, fmt.Errorf("node extension capabilities: %w", err)
	}
	bootID, bootErr := hostlinux.CurrentBootID()
	configDigest := observedConfigDigest(cfg, extensions)
	providers := []nodecapabilitymanager.Provider{
		configCapabilityProvider(extensions, configDigest),
		networkCapabilityProvider(cfg, configDigest),
		cgroupCapabilityProvider(cfg, cgroupRoot, bootID, bootErr),
		filestoreCapabilityProvider(cfg, bootID, bootErr),
		erofsCapabilityProvider(cfg, bootID, bootErr),
		runtimeConformanceCapabilityProvider(cfg, h.runtimeHandlers, config.RuntimeNameRunc, runtimeConformanceKindMemory, bootID, h.runRuntimeConformanceSelfTest),
		runtimeConformanceCapabilityProvider(cfg, h.runtimeHandlers, config.RuntimeNameRunc, runtimeConformanceKindEphemeral, bootID, h.runRuntimeConformanceSelfTest),
		runtimeConformanceCapabilityProvider(cfg, h.runtimeHandlers, config.RuntimeNameRunsc, runtimeConformanceKindMemory, bootID, h.runRuntimeConformanceSelfTest),
		runtimeConformanceCapabilityProvider(cfg, h.runtimeHandlers, config.RuntimeNameRunsc, runtimeConformanceKindEphemeral, bootID, h.runRuntimeConformanceSelfTest),
		derivedCapabilityProvider{expected: []*capabilityv1.CapabilityKey{
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT),
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT),
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT),
			capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT),
		}},
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
				out = append(out, availableObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_CONFIG_STATIC, &capabilityv1.CapabilityEvidence{ConfigDigest: digest}))
			}
			return out, nil
		},
	}
}

func networkCapabilityProvider(cfg config.Config, digest string) nodecapabilitymanager.Provider {
	networkCapability := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE
	if cfg.PluginConfig.NetworkConfig.NatBackend == config.NatBackendEBPF {
		networkCapability = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET
	}
	keys := []*capabilityv1.CapabilityKey{
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING),
		capabilitycontract.PlatformKey(networkCapability),
	}
	return observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		expected: keys,
		observe: func(_ context.Context, now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			manager := networkmanager.NetworkManagers[cfg.PluginConfig.NetworkConfig.NatBackend]
			probe, ok := manager.(networkmanager.HealthProber)
			evidence := &capabilityv1.CapabilityEvidence{ConfigDigest: digest}
			if !ok {
				return refreshableFailedObservations(keys, now, evidence, "network backend has no health probe"), nil
			}
			health, err := probe.ProbeHealth(cfg.PluginConfig.NetworkConfig.IPRange)
			if err != nil {
				return refreshableUnknownObservations(keys, now, evidence, err.Error()), nil
			}
			return []*capabilityv1.CapabilityObservation{
				refreshableBoolObservation(keys[0], health.PortForwardingReady, now, evidence, "port-forwarding dataplane is unavailable"),
				refreshableBoolObservation(keys[1], health.NativeDataplaneReady, now, evidence, "network dataplane is unavailable"),
			}, nil
		},
	}
}

func cgroupCapabilityProvider(cfg config.Config, rootName, bootID string, bootErr error) nodecapabilitymanager.Provider {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER)
	return &startupProvider{delegate: observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_HOST_CGROUP,
		expected: []*capabilityv1.CapabilityKey{key},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			if bootErr != nil {
				return []*capabilityv1.CapabilityObservation{unknownCapabilityObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_BOOT, nil, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, bootErr.Error())}, nil
			}
			mode, err := cfg.PluginConfig.RuntimeConfig.CgroupEnforcementMode()
			if err != nil {
				return nil, err
			}
			evidence := &capabilityv1.CapabilityEvidence{BootID: bootID}
			if mode != config.CgroupEnforcementRequired {
				return []*capabilityv1.CapabilityObservation{failedObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_BOOT, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DISABLED, "cgroup enforcement is disabled for development")}, nil
			}
			if err := hostlinux.ProbeCgroupMemoryLimit(rootName); err != nil {
				return []*capabilityv1.CapabilityObservation{failedObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_BOOT, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, err.Error())}, nil
			}
			return []*capabilityv1.CapabilityObservation{availableObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_BOOT, evidence)}, nil
		},
	}}
}

func filestoreCapabilityProvider(cfg config.Config, bootID string, bootErr error) nodecapabilitymanager.Provider {
	keys := []*capabilityv1.CapabilityKey{
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER),
		capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA),
	}
	return observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE,
		expected: keys,
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			if bootErr != nil {
				return unknownCapabilityObservations(keys, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, nil, bootErr.Error()), nil
			}
			filestore := strings.TrimSpace(cfg.PluginConfig.RuntimeConfig.FilestoreDir)
			if filestore == "" {
				return failedObservations(keys, &capabilityv1.CapabilityEvidence{BootID: bootID}, "runtime filestore is not configured"), nil
			}
			facts, err := hostlinux.ReadFilestoreCapabilities(filestore)
			if err != nil {
				return unknownCapabilityObservations(keys, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, &capabilityv1.CapabilityEvidence{BootID: bootID}, err.Error()), nil
			}
			evidence := &capabilityv1.CapabilityEvidence{BootID: bootID, MountIdentity: facts.MountIdentity}
			return []*capabilityv1.CapabilityObservation{
				boolObservation(keys[0], facts.OverlayReady, evidence, "OverlayFS upper probe failed"),
				boolObservation(keys[1], facts.ProjectQuotaReady, evidence, "XFS project quota probe failed"),
			}, nil
		},
	}
}

func erofsCapabilityProvider(cfg config.Config, bootID string, bootErr error) nodecapabilitymanager.Provider {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS)
	return observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_EROFS_PROBE,
		expected: []*capabilityv1.CapabilityKey{key},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			evidence := &capabilityv1.CapabilityEvidence{BootID: bootID}
			if bootErr != nil {
				return []*capabilityv1.CapabilityObservation{unknownCapabilityObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, bootErr.Error())}, nil
			}
			filestore := strings.TrimSpace(cfg.PluginConfig.RuntimeConfig.FilestoreDir)
			facts, err := hostlinux.ReadFilestoreCapabilities(filestore)
			if err != nil {
				return []*capabilityv1.CapabilityObservation{unknownCapabilityObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, err.Error())}, nil
			}
			evidence.MountIdentity = facts.MountIdentity
			return []*capabilityv1.CapabilityObservation{boolObservation(key, facts.EROFSReady, evidence, firstNonEmptyCapabilityReason(facts.EROFSProbeError, "EROFS lower probe failed"))}, nil
		},
	}
}

func availableObservation(key *capabilityv1.CapabilityKey, scope capabilityv1.CapabilityValidityScope, evidence *capabilityv1.CapabilityEvidence) *capabilityv1.CapabilityObservation {
	return &capabilityv1.CapabilityObservation{Key: proto.Clone(key).(*capabilityv1.CapabilityKey), State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ValidityScope: scope, Evidence: proto.Clone(evidence).(*capabilityv1.CapabilityEvidence), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE}
}

func failedObservation(key *capabilityv1.CapabilityKey, scope capabilityv1.CapabilityValidityScope, evidence *capabilityv1.CapabilityEvidence, code capabilityv1.CapabilityReasonCode, reason string) *capabilityv1.CapabilityObservation {
	if evidence == nil {
		evidence = &capabilityv1.CapabilityEvidence{}
	}
	return &capabilityv1.CapabilityObservation{Key: proto.Clone(key).(*capabilityv1.CapabilityKey), State: capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE, ValidityScope: scope, Evidence: proto.Clone(evidence).(*capabilityv1.CapabilityEvidence), ReasonCode: code, Reason: reason}
}

func unknownCapabilityObservation(key *capabilityv1.CapabilityKey, scope capabilityv1.CapabilityValidityScope, evidence *capabilityv1.CapabilityEvidence, code capabilityv1.CapabilityReasonCode, reason string) *capabilityv1.CapabilityObservation {
	observation := failedObservation(key, scope, evidence, code, reason)
	observation.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN
	return observation
}

func unknownCapabilityObservations(keys []*capabilityv1.CapabilityKey, scope capabilityv1.CapabilityValidityScope, evidence *capabilityv1.CapabilityEvidence, reason string) []*capabilityv1.CapabilityObservation {
	out := make([]*capabilityv1.CapabilityObservation, 0, len(keys))
	for _, key := range keys {
		out = append(out, unknownCapabilityObservation(key, scope, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, reason))
	}
	return out
}

func boolObservation(key *capabilityv1.CapabilityKey, available bool, evidence *capabilityv1.CapabilityEvidence, reason string) *capabilityv1.CapabilityObservation {
	if available {
		return availableObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, evidence)
	}
	return failedObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, reason)
}

func failedObservations(keys []*capabilityv1.CapabilityKey, evidence *capabilityv1.CapabilityEvidence, reason string) []*capabilityv1.CapabilityObservation {
	out := make([]*capabilityv1.CapabilityObservation, 0, len(keys))
	for _, key := range keys {
		out = append(out, failedObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, reason))
	}
	return out
}

func refreshableBoolObservation(key *capabilityv1.CapabilityKey, available bool, now time.Time, evidence *capabilityv1.CapabilityEvidence, reason string) *capabilityv1.CapabilityObservation {
	if available {
		observation := availableObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, evidence)
		observation.ValidUntil = timestamppb.New(now.Add(10 * time.Second))
		return observation
	}
	observation := failedObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, reason)
	observation.ValidUntil = timestamppb.New(now)
	return observation
}

func refreshableFailedObservations(keys []*capabilityv1.CapabilityKey, now time.Time, evidence *capabilityv1.CapabilityEvidence, reason string) []*capabilityv1.CapabilityObservation {
	result := make([]*capabilityv1.CapabilityObservation, 0, len(keys))
	for _, key := range keys {
		result = append(result, refreshableBoolObservation(key, false, now, evidence, reason))
	}
	return result
}

func refreshableUnknownObservations(keys []*capabilityv1.CapabilityKey, now time.Time, evidence *capabilityv1.CapabilityEvidence, reason string) []*capabilityv1.CapabilityObservation {
	result := unknownCapabilityObservations(keys, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, evidence, reason)
	for _, observation := range result {
		observation.ValidUntil = timestamppb.New(now)
	}
	return result
}

func derivedObservation(platform capabilityv1.PlatformCapability, now time.Time, dependencies ...*capabilityv1.CapabilityObservation) *capabilityv1.CapabilityObservation {
	key := capabilitycontract.PlatformKey(platform)
	references := make([]*capabilityv1.CapabilityEvidenceReference, 0, len(dependencies))
	available := true
	for _, dependency := range dependencies {
		if dependency == nil {
			available = false
			continue
		}
		if dependency.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
			available = false
		}
		references = append(references, &capabilityv1.CapabilityEvidenceReference{
			Key: proto.Clone(dependency.GetKey()).(*capabilityv1.CapabilityKey), EvidenceID: dependency.GetEvidence().GetEvidenceID(),
			Evidence: proto.Clone(dependency.GetEvidence()).(*capabilityv1.CapabilityEvidence),
		})
	}
	if !available {
		failed := failedObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, &capabilityv1.CapabilityEvidence{}, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE, "one or more capability dependencies are unavailable")
		failed.ValidUntil = timestamppb.New(now)
		failed.Dependencies = references
		return failed
	}
	return &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		ValidityScope: capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE,
		ValidUntil:    timestamppb.New(now.Add(10 * time.Second)), Dependencies: references,
		Evidence:   &capabilityv1.CapabilityEvidence{ConfigDigest: "derived-v1"},
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
}

func observedConfigDigest(cfg config.Config, extensions []*capabilityv1.ExtensionCapability) string {
	payload, _ := json.Marshal(struct {
		NAT        string                              `json:"nat"`
		Extensions []*capabilityv1.ExtensionCapability `json:"extensions"`
	}{NAT: cfg.PluginConfig.NetworkConfig.NatBackend, Extensions: extensions})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func firstNonEmptyCapabilityReason(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "capability probe failed"
}
