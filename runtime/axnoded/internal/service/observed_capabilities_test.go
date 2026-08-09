package service

import (
	"context"
	"net"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	capabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

func TestConfigCapabilityProviderPublishesOnlyExtensionFacts(t *testing.T) {
	extension := &capabilityv1.ExtensionCapability{Name: "example.com/accelerator", Value: "v1"}
	provider := configCapabilityProvider([]*capabilityv1.ExtensionCapability{extension}, "config-digest")
	observations, err := provider.Observe(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	wants := []*capabilityv1.CapabilityKey{capabilitycontract.ExtensionKey(extension.GetName(), extension.GetValue())}
	if len(observations) != len(wants) {
		t.Fatalf("observations = %#v", observations)
	}
	for _, want := range wants {
		wantID, _ := capabilitycontract.KeyID(want)
		found := false
		for _, observation := range observations {
			gotID, _ := capabilitycontract.KeyID(observation.GetKey())
			if gotID == wantID && observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
				found = true
			}
		}
		if !found {
			t.Fatalf("typed capability %q was not published: %#v", wantID, observations)
		}
	}
}

type observedNetworkManager struct{ health networkmanager.Health }

func (m observedNetworkManager) ProbeHealth(string) (networkmanager.Health, error) {
	return m.health, nil
}
func (observedNetworkManager) SetupSNATRules(string) error                          { return nil }
func (observedNetworkManager) CleanupSNATRules(string) error                        { return nil }
func (observedNetworkManager) SetupNetworkRulesForActivating(net.IP, string) error  { return nil }
func (observedNetworkManager) CleanupNetworkRulesForActivating(net.IP) error        { return nil }
func (observedNetworkManager) SetupDNATRule(string, uint16, string, uint16) error   { return nil }
func (observedNetworkManager) CleanupDNATRule(string, uint16, string, uint16) error { return nil }

func TestNetworkCapabilityProviderRequiresObservedDataplaneHealth(t *testing.T) {
	const backend = "observed-test"
	networkmanager.NetworkManagers[backend] = observedNetworkManager{health: networkmanager.Health{PortForwardingReady: true, NativeDataplaneReady: false}}
	t.Cleanup(func() { delete(networkmanager.NetworkManagers, backend) })
	provider := networkCapabilityProvider(config.Config{PluginConfig: config.PluginConfig{NetworkConfig: config.NetworkConfig{NatBackend: backend, IPRange: "172.17.0.1/16"}}}, "config-digest")
	observations, err := provider.Observe(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if observations[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE || observations[1].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE {
		t.Fatalf("network observations = %#v", observations)
	}
}

func TestDerivedCapabilityUsesRecoveryFilteredDependencies(t *testing.T) {
	cgroupKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER)
	selfTestKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST)
	derivedKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT)
	available := false
	cgroup := observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_HOST_CGROUP,
		expected: []*capabilityv1.CapabilityKey{cgroupKey},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			if !available {
				return []*capabilityv1.CapabilityObservation{failedObservation(cgroupKey, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_BOOT, &capabilityv1.CapabilityEvidence{BootID: "boot"}, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, "failed")}, nil
			}
			return []*capabilityv1.CapabilityObservation{availableObservation(cgroupKey, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_BOOT, &capabilityv1.CapabilityEvidence{BootID: "boot"})}, nil
		},
	}
	selfTest := observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST,
		expected: []*capabilityv1.CapabilityKey{selfTestKey},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			evidence := &capabilityv1.CapabilityEvidence{BootID: "boot", RuntimeName: "runc", RuntimeBinaryDigest: "binary", ConfigDigest: "config"}
			if !available {
				return []*capabilityv1.CapabilityObservation{failedObservation(selfTestKey, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, "failed")}, nil
			}
			return []*capabilityv1.CapabilityObservation{availableObservation(selfTestKey, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, evidence)}, nil
		},
	}
	manager, err := capabilitymanager.NewManager(cgroup, selfTest, derivedCapabilityProvider{expected: []*capabilityv1.CapabilityKey{derivedKey}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	available = true
	first, err := manager.Refresh(context.Background(), now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("first recovery refresh must publish a coherent snapshot: %v", err)
	}
	if observation, ok := capabilitycontract.AvailableObservation(first, derivedKey, now.Add(5*time.Second)); ok || observation != nil {
		t.Fatal("derived capability became available before base recovery was confirmed")
	}
	second, err := manager.Refresh(context.Background(), now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("second recovery refresh: %v", err)
	}
	if _, ok := capabilitycontract.AvailableObservation(second, derivedKey, now.Add(10*time.Second)); !ok {
		t.Fatal("derived capability did not recover with its confirmed base dependencies")
	}
}
