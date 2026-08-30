package config

import (
	"reflect"
	"testing"
)

func TestRuntimeConfigNormalizedRuntimeConfigs(t *testing.T) {
	cfg := RuntimeConfig{
		RuntimeBinary: map[string]string{
			RuntimeNameRunsc: "/legacy/runsc",
			"runc":           "/legacy/runc",
		},
		BasicSpec: map[string]string{
			RuntimeNameRunsc: "/legacy/runsc.json",
		},
		Runtimes: map[string]RuntimeInstanceConfig{
			RuntimeNameRunsc: {
				Binary: "/new/runsc",
			},
			"crun": {
				Binary:   "/usr/bin/crun",
				BaseSpec: "/etc/axnoded/crun.json",
			},
		},
	}

	runtimes := cfg.NormalizedRuntimeConfigs()
	if len(runtimes) != 3 {
		t.Fatalf("expected 3 runtimes, got %d", len(runtimes))
	}

	runsc := runtimes[RuntimeNameRunsc]
	if runsc.Binary != "/new/runsc" {
		t.Fatalf("expected new binary to win, got %q", runsc.Binary)
	}
	if runsc.BaseSpec != "/legacy/runsc.json" {
		t.Fatalf("expected legacy base spec fallback, got %q", runsc.BaseSpec)
	}
	if !runsc.Options.AllowSUIDEnabled(true) {
		t.Fatalf("expected runsc allow_suid default to remain enabled")
	}

	runc := runtimes["runc"]
	if runc.Binary != "/legacy/runc" {
		t.Fatalf("expected legacy runtime binary, got %q", runc.Binary)
	}

	crun := runtimes["crun"]
	if crun.Binary != "/usr/bin/crun" {
		t.Fatalf("expected explicit crun binary, got %q", crun.Binary)
	}
	if crun.BaseSpec != "/etc/axnoded/crun.json" {
		t.Fatalf("expected explicit crun base spec, got %q", crun.BaseSpec)
	}
}

func TestNetworkConfigNormalizedCanonicalizesSemanticSetsAndDurations(t *testing.T) {
	input := DefaultConfig().PluginConfig.NetworkConfig
	input.NatBackend = " EBPF "
	input.IPRange = "172.17.0.1/16"
	input.BPFNet.UplinkDevices = []string{" eth1 ", "eth0"}
	input.BPFNet.NativeRoutingCIDRs = []string{"10.2.3.4/16", "10.0.0.0/8"}
	input.BPFNet.SNATGCInterval = "1000ms"

	got, err := input.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if got.NatBackend != NatBackendEBPF || got.BPFNet.SNATGCInterval != "1s" {
		t.Fatalf("normalized scalar values = %#v", got)
	}
	if want := []string{"eth0", "eth1"}; !reflect.DeepEqual(got.BPFNet.UplinkDevices, want) {
		t.Fatalf("uplink devices = %#v, want %#v", got.BPFNet.UplinkDevices, want)
	}
	if want := []string{"10.0.0.0/8", "10.2.0.0/16"}; !reflect.DeepEqual(got.BPFNet.NativeRoutingCIDRs, want) {
		t.Fatalf("native routing CIDRs = %#v, want %#v", got.BPFNet.NativeRoutingCIDRs, want)
	}
}

func TestNetworkConfigNormalizedRejectsAmbiguousOrInvalidValues(t *testing.T) {
	tests := map[string]func(*NetworkConfig){
		"unsupported backend": func(c *NetworkConfig) { c.NatBackend = "custom" },
		"invalid IP range":    func(c *NetworkConfig) { c.IPRange = "not-a-prefix" },
		"duplicate uplink": func(c *NetworkConfig) {
			c.NatBackend = NatBackendEBPF
			c.BPFNet.UplinkDevices = []string{"eth0", " eth0 "}
		},
		"duplicate canonical CIDR": func(c *NetworkConfig) {
			c.NatBackend = NatBackendEBPF
			c.BPFNet.NativeRoutingCIDRs = []string{"10.0.0.1/8", "10.0.0.0/8"}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := DefaultConfig().PluginConfig.NetworkConfig
			mutate(&cfg)
			if _, err := cfg.Normalized(); err == nil {
				t.Fatal("invalid network configuration was accepted")
			}
		})
	}
}

func TestRuntimeConfigCgroupEnforcementMode(t *testing.T) {
	mode, err := (RuntimeConfig{}).CgroupEnforcementMode()
	if err != nil || mode != CgroupEnforcementRequired {
		t.Fatalf("default mode = %q, %v", mode, err)
	}
	mode, err = (RuntimeConfig{CgroupEnforcement: CgroupEnforcementDisabledDev}).CgroupEnforcementMode()
	if err != nil || mode != CgroupEnforcementDisabledDev {
		t.Fatalf("dev mode = %q, %v", mode, err)
	}
	if _, err := (RuntimeConfig{CgroupEnforcement: "fallback"}).CgroupEnforcementMode(); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestResourceConfigCgroupRootNameIsDelegatedChild(t *testing.T) {
	if got, err := (ResourceConfig{}).CgroupRootNameValue(); err != nil || got != "sandbox" {
		t.Fatalf("default cgroup root = %q, %v", got, err)
	}
	if got, err := (ResourceConfig{CgroupRootName: "tenant-sandbox"}).CgroupRootNameValue(); err != nil || got != "tenant-sandbox" {
		t.Fatalf("custom cgroup root = %q, %v", got, err)
	}
	for _, invalid := range []string{"/sandbox", "parent/sandbox", "..", "internal", "workload", "conformance"} {
		if _, err := (ResourceConfig{CgroupRootName: invalid}).CgroupRootNameValue(); err == nil {
			t.Fatalf("invalid cgroup root %q accepted", invalid)
		}
	}
}

func TestRuntimeOptionsAllowSUIDEnabled(t *testing.T) {
	if !(RuntimeOptions{}).AllowSUIDEnabled(true) {
		t.Fatal("expected nil allow_suid option to use true default")
	}
	if (RuntimeOptions{}).AllowSUIDEnabled(false) {
		t.Fatal("expected nil allow_suid option to use false default")
	}
	if (RuntimeOptions{AllowSUID: boolPtr(false)}).AllowSUIDEnabled(true) {
		t.Fatal("expected explicit allow_suid=false to win")
	}
	if !(RuntimeOptions{AllowSUID: boolPtr(true)}).AllowSUIDEnabled(false) {
		t.Fatal("expected explicit allow_suid=true to win")
	}
}

func TestDefaultConfigSetsImageManagerSocket(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.PluginConfig.RuntimeConfig.ImageManagerEnabledValue() {
		t.Fatal("expected image manager to be enabled by default")
	}
	if cfg.PluginConfig.RuntimeConfig.ImageManagerSocket != DefaultImageManagerSocket {
		t.Fatalf("expected default image manager socket %q, got %q",
			DefaultImageManagerSocket, cfg.PluginConfig.RuntimeConfig.ImageManagerSocket)
	}
	if cfg.PluginConfig.RuntimeConfig.ImageManagerSocketPath() != DefaultImageManagerSocket {
		t.Fatalf("expected default image manager socket path %q, got %q",
			DefaultImageManagerSocket, cfg.PluginConfig.RuntimeConfig.ImageManagerSocketPath())
	}
}

func TestDefaultConfigSetsVolumeManagerSocket(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PluginConfig.RuntimeConfig.VolumeManagerSocket != DefaultVolumeManagerSocket {
		t.Fatalf("expected volume manager socket %q, got %q", DefaultVolumeManagerSocket, cfg.PluginConfig.RuntimeConfig.VolumeManagerSocket)
	}
	if cfg.PluginConfig.RuntimeConfig.VolumeManagerSocketPath() != DefaultVolumeManagerSocket {
		t.Fatalf("expected volume manager socket path %q, got %q", DefaultVolumeManagerSocket, cfg.PluginConfig.RuntimeConfig.VolumeManagerSocketPath())
	}
	if cfg.PluginConfig.RuntimeConfig.EgressManagerSocketPath() != DefaultEgressManagerSocket {
		t.Fatalf("expected egress manager socket path %q, got %q", DefaultEgressManagerSocket, cfg.PluginConfig.RuntimeConfig.EgressManagerSocketPath())
	}
}

func TestDefaultConfigSetsRuntimeRunnerBinary(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PluginConfig.RuntimeConfig.RuntimeRunnerBinary != DefaultRuntimeRunnerBinary {
		t.Fatalf("expected runtime runner binary %q, got %q",
			DefaultRuntimeRunnerBinary, cfg.PluginConfig.RuntimeConfig.RuntimeRunnerBinary)
	}
	if cfg.PluginConfig.RuntimeConfig.RuntimeRunnerBinaryPath() != DefaultRuntimeRunnerBinary {
		t.Fatalf("expected runtime runner binary path %q, got %q",
			DefaultRuntimeRunnerBinary, cfg.PluginConfig.RuntimeConfig.RuntimeRunnerBinaryPath())
	}
}

func TestDefaultConfigEnablesRunscSUID(t *testing.T) {
	cfg := DefaultConfig()
	runsc, ok := cfg.PluginConfig.RuntimeConfig.NormalizedRuntimeConfig(RuntimeNameRunsc)
	if !ok {
		t.Fatal("expected default runsc runtime")
	}
	if !runsc.Options.AllowSUIDEnabled(false) {
		t.Fatal("expected default runsc runtime to enable setuid binaries")
	}
}

func TestDefaultConfigLeavesRuntimeDNSDerivedFromNode(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.PluginConfig.RuntimeConfig.DNS.Nameservers) != 0 {
		t.Fatalf("expected default runtime DNS nameservers to be empty, got %v", cfg.PluginConfig.RuntimeConfig.DNS.Nameservers)
	}
}

func TestRuntimeConfigImageManagerEnabledValue(t *testing.T) {
	t.Run("explicitly disabled", func(t *testing.T) {
		cfg := RuntimeConfig{
			ImageManagerEnabled: boolPtr(false),
			ImageManagerSocket:  DefaultImageManagerSocket,
		}
		if cfg.ImageManagerEnabledValue() {
			t.Fatal("expected image manager to be disabled")
		}
		if cfg.ImageManagerSocketPath() != "" {
			t.Fatalf("expected empty socket path when disabled, got %q", cfg.ImageManagerSocketPath())
		}
	})

	t.Run("zero value defaults to enabled", func(t *testing.T) {
		cfg := RuntimeConfig{}
		if !cfg.ImageManagerEnabledValue() {
			t.Fatal("expected zero-value runtime config to keep image manager enabled")
		}
		if cfg.ImageManagerSocketPath() != DefaultImageManagerSocket {
			t.Fatalf("expected default socket path %q, got %q", DefaultImageManagerSocket, cfg.ImageManagerSocketPath())
		}
	})
}

func TestDefaultConfigSetsIdleRuntimeRetentionDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PluginConfig.RuntimeConfig.IdleRuntimeRetentionTTL != DefaultIdleRuntimeRetentionTTL {
		t.Fatalf("expected idle runtime retention ttl %q, got %q",
			DefaultIdleRuntimeRetentionTTL, cfg.PluginConfig.RuntimeConfig.IdleRuntimeRetentionTTL)
	}
	if cfg.PluginConfig.RuntimeConfig.IdleRuntimeRetentionMax == nil {
		t.Fatal("expected default idle runtime retention max pointer to be populated")
	}
	if *cfg.PluginConfig.RuntimeConfig.IdleRuntimeRetentionMax != DefaultIdleRuntimeRetentionMax {
		t.Fatalf("expected idle runtime retention max %d, got %d",
			DefaultIdleRuntimeRetentionMax, *cfg.PluginConfig.RuntimeConfig.IdleRuntimeRetentionMax)
	}
	ttl, err := cfg.PluginConfig.RuntimeConfig.IdleRuntimeRetentionTTLDuration()
	if err != nil {
		t.Fatalf("parse default idle runtime retention ttl: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("expected positive idle runtime retention ttl, got %v", ttl)
	}
}

func TestDefaultConfigSetsResourcePoolReconcileIntervalDefault(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.PluginConfig.ResourceConfig.ResourcePoolReconcileInterval; got != DefaultResourcePoolReconcileInterval {
		t.Fatalf("expected resource pool reconcile interval %q, got %q", DefaultResourcePoolReconcileInterval, got)
	}
	interval, err := cfg.PluginConfig.ResourceConfig.ResourcePoolReconcileIntervalDuration()
	if err != nil {
		t.Fatalf("parse default resource pool reconcile interval: %v", err)
	}
	if interval <= 0 {
		t.Fatalf("expected positive reconcile interval, got %v", interval)
	}
}

func TestDefaultConfigSetsControlPlaneHeartbeatIntervalDefault(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.PluginConfig.ControlPlaneHeartbeatInterval; got != DefaultControlPlaneHeartbeatInterval {
		t.Fatalf("expected control plane heartbeat interval %q, got %q", DefaultControlPlaneHeartbeatInterval, got)
	}
	interval, err := cfg.PluginConfig.ControlPlaneHeartbeatIntervalDuration()
	if err != nil {
		t.Fatalf("parse default control plane heartbeat interval: %v", err)
	}
	if interval <= 0 {
		t.Fatalf("expected positive heartbeat interval, got %v", interval)
	}
	if got := cfg.PluginConfig.ControlPlaneNodeState; got != DefaultControlPlaneNodeState {
		t.Fatalf("expected control plane node state %q, got %q", DefaultControlPlaneNodeState, got)
	}
}

func TestPluginConfigControlPlaneHelpers(t *testing.T) {
	cfg := PluginConfig{
		ControlPlaneTarget:            " 127.0.0.1:24000 ",
		ControlPlaneNodeID:            "",
		ControlPlaneHeartbeatInterval: "0s",
	}
	if got := cfg.ControlPlaneTargetValue(); got != "127.0.0.1:24000" {
		t.Fatalf("ControlPlaneTargetValue() = %q, want %q", got, "127.0.0.1:24000")
	}
	if got := cfg.ControlPlaneNodeIDValue("host-a"); got != "host-a" {
		t.Fatalf("ControlPlaneNodeIDValue() = %q, want host-a", got)
	}
	interval, err := cfg.ControlPlaneHeartbeatIntervalDuration()
	if err != nil {
		t.Fatalf("ControlPlaneHeartbeatIntervalDuration() error = %v", err)
	}
	if interval.String() != DefaultControlPlaneHeartbeatInterval {
		t.Fatalf("ControlPlaneHeartbeatIntervalDuration() = %v, want %v", interval, DefaultControlPlaneHeartbeatInterval)
	}
	if got := cfg.ControlPlaneNodeStateValue(); got != DefaultControlPlaneNodeState {
		t.Fatalf("ControlPlaneNodeStateValue() = %q, want %q", got, DefaultControlPlaneNodeState)
	}

	cfg.ControlPlaneNodeState = "draining"
	cfg.ControlPlaneNodeResourceSource = " kubernetes "
	cfg.ControlPlaneKubernetesNodeName = " node-a "
	cfg.NodeExtensionCapabilities = []ExtensionCapabilityConfig{{Name: " example.com/accelerator ", Value: " v1 "}}
	cfg.ControlPlaneNodeLabels = map[string]string{
		" zone ": " us-east-1 ",
		"":       "ignored",
	}
	if got := cfg.ControlPlaneNodeStateValue(); got != "draining" {
		t.Fatalf("ControlPlaneNodeStateValue() = %q, want draining", got)
	}
	if _, err := cfg.NodeExtensionCapabilitiesValue(); err == nil {
		t.Fatal("NodeExtensionCapabilitiesValue accepted surrounding name whitespace")
	}
	cfg.NodeExtensionCapabilities = []ExtensionCapabilityConfig{{Name: "Example.COM/accelerator", Value: " v1 "}}
	capabilities, err := cfg.NodeExtensionCapabilitiesValue()
	if err != nil || len(capabilities) != 1 || capabilities[0].GetName() != "example.com/accelerator" || capabilities[0].GetValue() != " v1 " {
		t.Fatalf("NodeExtensionCapabilitiesValue() = %#v, %v", capabilities, err)
	}
	cfg.NodeExtensionCapabilities = []ExtensionCapabilityConfig{{Name: "example.com/accelerator", Value: "v1"}, {Name: "example.com/accelerator", Value: "v2"}}
	if _, err := cfg.NodeExtensionCapabilitiesValue(); err == nil {
		t.Fatal("NodeExtensionCapabilitiesValue accepted multiple values for one qualified name")
	}
	cfg.NodeExtensionCapabilities = nil
	labels := cfg.ControlPlaneNodeLabelsValue()
	if len(labels) != 1 || labels["zone"] != "us-east-1" {
		t.Fatalf("ControlPlaneNodeLabelsValue() = %#v, want zone=us-east-1", labels)
	}
	source, err := cfg.ControlPlaneNodeResourceSourceValue()
	if err != nil {
		t.Fatalf("ControlPlaneNodeResourceSourceValue() error = %v", err)
	}
	if source != ControlPlaneNodeResourceSourceKubernetes {
		t.Fatalf("ControlPlaneNodeResourceSourceValue() = %q, want kubernetes", source)
	}
	if got := cfg.ControlPlaneKubernetesNodeNameValue("fallback"); got != "node-a" {
		t.Fatalf("ControlPlaneKubernetesNodeNameValue() = %q, want node-a", got)
	}
	cfg.ControlPlaneNodeResourceSource = "unknown"
	if _, err := cfg.ControlPlaneNodeResourceSourceValue(); err == nil {
		t.Fatal("expected unknown ControlPlaneNodeResourceSourceValue() to fail")
	}
	cfg.ControlPlaneKubernetesNodeName = ""
	if got := cfg.ControlPlaneKubernetesNodeNameValue("fallback"); got != "fallback" {
		t.Fatalf("fallback ControlPlaneKubernetesNodeNameValue() = %q, want fallback", got)
	}
}

func TestResourcePoolReconcileIntervalDurationSupportsExplicitZero(t *testing.T) {
	cfg := ResourceConfig{ResourcePoolReconcileInterval: "0s"}
	interval, err := cfg.ResourcePoolReconcileIntervalDuration()
	if err != nil {
		t.Fatalf("ResourcePoolReconcileIntervalDuration() error = %v", err)
	}
	if interval.String() != DefaultResourcePoolReconcileInterval {
		t.Fatalf("ResourcePoolReconcileIntervalDuration() = %v, want %v", interval, DefaultResourcePoolReconcileInterval)
	}
}

func TestRuntimeConfigIdleRuntimeRetentionMaxValueSupportsExplicitZero(t *testing.T) {
	disabled := 0
	cfg := RuntimeConfig{IdleRuntimeRetentionMax: &disabled}
	if got := cfg.IdleRuntimeRetentionMaxValue(); got != 0 {
		t.Fatalf("IdleRuntimeRetentionMaxValue() = %d, want 0", got)
	}
}

func TestDefaultConfigSetsBPFNetDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PluginConfig.NetworkConfig.BPFNet.PinPath != DefaultBPFNetPinPath {
		t.Fatalf("expected default bpfnet pin path %q, got %q",
			DefaultBPFNetPinPath, cfg.PluginConfig.NetworkConfig.BPFNet.PinPath)
	}
	if cfg.PluginConfig.NetworkConfig.BPFNet.MapSize != DefaultBPFNetMapSize {
		t.Fatalf("expected default bpfnet map size %d, got %d",
			DefaultBPFNetMapSize, cfg.PluginConfig.NetworkConfig.BPFNet.MapSize)
	}
	if cfg.PluginConfig.NetworkConfig.BPFNet.SNATMapSize != DefaultBPFNetSNATMapSize {
		t.Fatalf("expected default bpfnet snat map size %d, got %d",
			DefaultBPFNetSNATMapSize, cfg.PluginConfig.NetworkConfig.BPFNet.SNATMapSize)
	}
	if cfg.PluginConfig.NetworkConfig.BPFNet.SNATGCInterval != DefaultBPFNetSNATGCInterval {
		t.Fatalf("expected default bpfnet snat gc interval %q, got %q",
			DefaultBPFNetSNATGCInterval, cfg.PluginConfig.NetworkConfig.BPFNet.SNATGCInterval)
	}
	if cfg.PluginConfig.NetworkConfig.BPFNet.SNATTCPIdleTimeout != DefaultBPFNetSNATTCPIdleTimeout {
		t.Fatalf("expected default bpfnet snat tcp idle timeout %q, got %q",
			DefaultBPFNetSNATTCPIdleTimeout, cfg.PluginConfig.NetworkConfig.BPFNet.SNATTCPIdleTimeout)
	}
	if cfg.PluginConfig.NetworkConfig.BPFNet.SNATTCPClosingTimeout != DefaultBPFNetSNATTCPClosingTimeout {
		t.Fatalf("expected default bpfnet snat tcp closing timeout %q, got %q",
			DefaultBPFNetSNATTCPClosingTimeout, cfg.PluginConfig.NetworkConfig.BPFNet.SNATTCPClosingTimeout)
	}
	if cfg.PluginConfig.NetworkConfig.BPFNet.SNATDatagramIdleTimeout != DefaultBPFNetSNATDatagramIdleTimeout {
		t.Fatalf("expected default bpfnet snat datagram idle timeout %q, got %q",
			DefaultBPFNetSNATDatagramIdleTimeout, cfg.PluginConfig.NetworkConfig.BPFNet.SNATDatagramIdleTimeout)
	}
	if !cfg.PluginConfig.NetworkConfig.BPFNet.LocalOutCompat {
		t.Fatalf("expected local_out_compat to default to true")
	}
	if !cfg.PluginConfig.NetworkConfig.BPFNet.IptablesFallback {
		t.Fatalf("expected iptables_fallback to default to true")
	}
	if interval, err := cfg.PluginConfig.NetworkConfig.BPFNet.SNATGCIntervalDuration(); err != nil || interval.String() != DefaultBPFNetSNATGCInterval {
		t.Fatalf("SNATGCIntervalDuration() = %v, %v; want %s", interval, err, DefaultBPFNetSNATGCInterval)
	}
	if timeout, err := cfg.PluginConfig.NetworkConfig.BPFNet.SNATTCPClosingTimeoutDuration(); err != nil || timeout.String() != DefaultBPFNetSNATTCPClosingTimeout {
		t.Fatalf("SNATTCPClosingTimeoutDuration() = %v, %v; want %s", timeout, err, DefaultBPFNetSNATTCPClosingTimeout)
	}
}
