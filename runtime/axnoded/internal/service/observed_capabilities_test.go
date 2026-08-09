package service

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	capabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

const testCapabilityBootID = "11111111-2222-3333-4444-555555555555"

func TestNetworkConfigDigestUsesCompleteNormalizedNetworkConfig(t *testing.T) {
	base, err := config.DefaultConfig().PluginConfig.NetworkConfig.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	equivalent := base
	equivalent.BPFNet.SNATGCInterval = "1000ms"
	equivalent.BPFNet.NativeRoutingCIDRs = []string{"10.2.3.4/16", "10.0.0.0/8"}
	base.BPFNet.NativeRoutingCIDRs = []string{"10.0.0.0/8", "10.2.0.0/16"}
	equivalent, err = equivalent.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	base, err = base.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	baseDigest, err := networkConfigDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	equivalentDigest, err := networkConfigDigest(equivalent)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(base, equivalent) || baseDigest != equivalentDigest {
		t.Fatal("semantically equivalent network configurations produced different evidence identity")
	}

	mutations := map[string]func(*config.NetworkConfig){
		"ip range":            func(c *config.NetworkConfig) { c.IPRange = "172.18.0.1/16" },
		"backend":             func(c *config.NetworkConfig) { c.NatBackend = config.NatBackendEBPF },
		"pin path":            func(c *config.NetworkConfig) { c.BPFNet.PinPath += "-changed" },
		"map size":            func(c *config.NetworkConfig) { c.BPFNet.MapSize++ },
		"snat map size":       func(c *config.NetworkConfig) { c.BPFNet.SNATMapSize++ },
		"gc interval":         func(c *config.NetworkConfig) { c.BPFNet.SNATGCInterval = "2s" },
		"tcp idle":            func(c *config.NetworkConfig) { c.BPFNet.SNATTCPIdleTimeout = "6m" },
		"tcp closing":         func(c *config.NetworkConfig) { c.BPFNet.SNATTCPClosingTimeout = "3s" },
		"datagram idle":       func(c *config.NetworkConfig) { c.BPFNet.SNATDatagramIdleTimeout = "11s" },
		"local-out compat":    func(c *config.NetworkConfig) { c.BPFNet.LocalOutCompat = !c.BPFNet.LocalOutCompat },
		"iptables fallback":   func(c *config.NetworkConfig) { c.BPFNet.IptablesFallback = !c.BPFNet.IptablesFallback },
		"uplink devices":      func(c *config.NetworkConfig) { c.BPFNet.UplinkDevices = []string{"eth0"} },
		"native routing CIDR": func(c *config.NetworkConfig) { c.BPFNet.NativeRoutingCIDRs = []string{"10.3.0.0/16"} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := base
			changed.BPFNet.UplinkDevices = append([]string(nil), base.BPFNet.UplinkDevices...)
			changed.BPFNet.NativeRoutingCIDRs = append([]string(nil), base.BPFNet.NativeRoutingCIDRs...)
			mutate(&changed)
			changedDigest, digestErr := networkConfigDigest(changed)
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			if changedDigest == baseDigest {
				t.Fatal("behavioral network configuration change did not change evidence identity")
			}
		})
	}
}

func TestExtensionConfigDigestIsIndependentFromNetworkConfig(t *testing.T) {
	extensions := []*capabilityv1.ExtensionCapability{{Name: "example.com/device", Value: "v1"}}
	want := extensionConfigDigest(extensions)
	network := config.DefaultConfig().PluginConfig.NetworkConfig
	network.IPRange = "172.19.0.1/16"
	if _, err := networkConfigDigest(network); err != nil {
		t.Fatal(err)
	}
	if got := extensionConfigDigest(extensions); got != want {
		t.Fatalf("network configuration changed extension evidence: got %q want %q", got, want)
	}
}

func TestConfigCapabilityProviderPublishesOnlyExtensionFacts(t *testing.T) {
	extension := &capabilityv1.ExtensionCapability{Name: "example.com/accelerator", Value: "v1"}
	provider := configCapabilityProvider([]*capabilityv1.ExtensionCapability{extension}, sha256Digest([]byte("extensions")))
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
	provider := networkCapabilityProvider(config.Config{PluginConfig: config.PluginConfig{NetworkConfig: config.NetworkConfig{NatBackend: backend, IPRange: "172.17.0.1/16"}}}, sha256Digest([]byte("network")))
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
				return []*capabilityv1.CapabilityObservation{failedObservation(cgroupKey, capabilitycontract.BootEvidence(testCapabilityBootID), capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, "failed")}, nil
			}
			return []*capabilityv1.CapabilityObservation{availableObservation(cgroupKey, capabilitycontract.BootEvidence(testCapabilityBootID))}, nil
		},
	}
	selfTest := observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST,
		expected: []*capabilityv1.CapabilityKey{selfTestKey},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			evidence := capabilitycontract.RuntimeEvidence(testCapabilityBootID, "runc", sha256Digest([]byte("binary")), sha256Digest([]byte("config")))
			if !available {
				return []*capabilityv1.CapabilityObservation{failedObservation(selfTestKey, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, "failed")}, nil
			}
			return []*capabilityv1.CapabilityObservation{availableObservation(selfTestKey, evidence)}, nil
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
		t.Fatal("derived capability did not recover after both base observations completed recovery confirmation")
	}
}
