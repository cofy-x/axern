//go:build !windows

package config

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cofy-x/axern/lib/go/executionlease"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

// Config contains all configurations for sandbox server.
type Config struct {
	// PluginConfig is the config for sandbox plugin.
	PluginConfig `toml:"plugin" json:"plugin"`
	// RootDir is the root directory path for managing sandbox service files
	// (metadata checkpoint etc.)
	RootDir string `json:"rootDir" toml:"rootDir"`
	// StoreDir is the root directory path for storing all necessary metadata.
	StoreDir string `json:"stateDir" toml:"storeDir"`
}

type PluginConfig struct {
	NetworkConfig `toml:"network" json:"network"`

	RuntimeConfig `toml:"runtime" json:"runtime"`

	ResourceConfig `toml:"resource" json:"resource"`

	ControlPlaneTarget             string                      `toml:"control_plane_target" json:"controlPlaneTarget"`
	ControlPlaneEnrollmentTarget   string                      `toml:"control_plane_enrollment_target" json:"controlPlaneEnrollmentTarget"`
	WorkloadCluster                string                      `toml:"workload_cluster" json:"workloadCluster"`
	ControlPlaneNodeID             string                      `toml:"control_plane_node_id" json:"controlPlaneNodeId"`
	ControlPlaneNodeTarget         string                      `toml:"control_plane_node_target" json:"controlPlaneNodeTarget"`
	ControlPlaneHeartbeatInterval  string                      `toml:"control_plane_heartbeat_interval" json:"controlPlaneHeartbeatInterval"`
	ControlPlaneNodeState          string                      `toml:"control_plane_node_state" json:"controlPlaneNodeState"`
	NodeExtensionCapabilities      []ExtensionCapabilityConfig `toml:"node_extension_capabilities" json:"nodeExtensionCapabilities"`
	ControlPlaneNodeLabels         map[string]string           `toml:"control_plane_node_labels" json:"controlPlaneNodeLabels"`
	ControlPlaneNodeResourceSource string                      `toml:"control_plane_node_resource_source" json:"controlPlaneNodeResourceSource"`
	ControlPlaneKubernetesNodeName string                      `toml:"control_plane_kubernetes_node_name" json:"controlPlaneKubernetesNodeName"`
	ControlPlaneTLSCACert          string                      `toml:"control_plane_tls_ca_cert" json:"controlPlaneTlsCaCert"`
}

type ExtensionCapabilityConfig struct {
	Name  string `toml:"name" json:"name"`
	Value string `toml:"value" json:"value"`
}

func (c Config) Validate() error {
	if err := c.ValidateNodeIdentity(); err != nil {
		return err
	}
	if strings.TrimSpace(c.PluginConfig.RuntimeConfig.RootfsSnapshotRepository) != "" && !c.PluginConfig.RuntimeConfig.ImageManagerEnabledValue() {
		return fmt.Errorf("rootfs_snapshot_repository requires image_manager_enabled")
	}
	return nil
}

// RuntimeConfig defines runsc and shared node execution policy.
type RuntimeConfig struct {
	Runsc RuntimeInstanceConfig `toml:"runsc" json:"runsc"`

	// CgroupEnforcement is "required" in production. "disabled_dev" is an
	// explicit development mode and rejects hard memory limits.
	CgroupEnforcement string `toml:"cgroup_enforcement" json:"cgroupEnforcement"`

	// ImageLibDir is the file to store image lib. Read image line by line.
	ImageLibDir string `toml:"image_lib_dir" json:"imageLibDir"`

	// ImageManagerEnabled controls whether axnoded should use imagemgr for
	// image-backed rootfs and inventory collection. Defaults to true. When false,
	// ImageManagerSocket is ignored.
	ImageManagerEnabled *bool `toml:"image_manager_enabled" json:"imageManagerEnabled"`

	// ImageManagerSocket points to the local imagemgr Unix socket.
	ImageManagerSocket string `toml:"image_manager_socket" json:"imageManagerSocket"`

	// RootfsSnapshotRepository is the platform-owned OCI repository used for
	// immutable Allocation rootfs snapshots. Empty disables the capability.
	RootfsSnapshotRepository string `toml:"rootfs_snapshot_repository" json:"rootfsSnapshotRepository"`

	// FilestoreDir is the validated root for runtime writable storage.
	FilestoreDir string `toml:"filestore_dir" json:"filestoreDir"`

	// FilestoreMode is "existing" for a production data-disk mount or
	// "loopback_dev" for an explicitly managed local-development image.
	FilestoreMode string `toml:"filestore_mode" json:"filestoreMode"`

	FilestoreLoopbackImage      string `toml:"filestore_loopback_image" json:"filestoreLoopbackImage"`
	FilestoreLoopbackSizeBytes  int64  `toml:"filestore_loopback_size_bytes" json:"filestoreLoopbackSizeBytes"`
	FilestoreSystemReserveBytes int64  `toml:"filestore_system_reserve_bytes" json:"filestoreSystemReserveBytes"`

	// EphemeralStorageDefaultLimitBytes is the required development-phase hard
	// size passed to runsc's file-backed root overlay.
	EphemeralStorageDefaultLimitBytes int64 `toml:"ephemeral_storage_default_limit_bytes" json:"ephemeralStorageDefaultLimitBytes"`

	// DNS controls the resolver files axnoded materializes into OCI bundles.
	// When nameservers is empty, axnoded derives usable resolvers from the
	// node's resolver configuration.
	DNS RuntimeDNSConfig `toml:"dns" json:"dns"`

	// EgressManagerSocket points to the trusted node-local egressd Unix socket.
	// Connectivity is observed as a capability and is required only when a Run
	// declares an enforced egress policy.
	EgressManagerSocket string `toml:"egress_manager_socket" json:"egressManagerSocket"`

	// IdleEnvironmentRetentionTTL controls how long prepared environments and
	// their rootfs remain retained after the last Allocation exits.
	IdleEnvironmentRetentionTTL string `toml:"idle_environment_retention_ttl" json:"idleEnvironmentRetentionTtl"`

	// IdleEnvironmentRetentionMax limits the number of prepared environments kept
	// warm at once. When <= 0, idle retention is disabled.
	IdleEnvironmentRetentionMax *int `toml:"idle_environment_retention_max" json:"idleEnvironmentRetentionMax"`
}

type RuntimeInstanceConfig struct {
	Binary   string         `toml:"binary" json:"binary"`
	BaseSpec string         `toml:"base_spec" json:"baseSpec"`
	Options  RuntimeOptions `toml:"options" json:"options"`
}

type RuntimeOptions struct {
	AllowSUID *bool `toml:"allow_suid" json:"allowSuid"`
}

type RuntimeDNSConfig struct {
	Nameservers   []string `toml:"nameservers" json:"nameservers"`
	SearchDomains []string `toml:"search_domains" json:"searchDomains"`
	Options       []string `toml:"options" json:"options"`
}

func (c RuntimeConfig) CgroupEnforcementMode() (string, error) {
	mode := strings.TrimSpace(c.CgroupEnforcement)
	if mode == "" {
		mode = CgroupEnforcementRequired
	}
	if mode != CgroupEnforcementRequired && mode != CgroupEnforcementDisabledDev {
		return "", fmt.Errorf("unsupported cgroup_enforcement %q", mode)
	}
	return mode, nil
}

func (o RuntimeOptions) AllowSUIDEnabled(defaultValue bool) bool {
	if o.AllowSUID == nil {
		return defaultValue
	}
	return *o.AllowSUID
}

func (c RuntimeConfig) ImageManagerEnabledValue() bool {
	return c.ImageManagerEnabled == nil || *c.ImageManagerEnabled
}

func (c RuntimeConfig) ImageManagerSocketPath() string {
	if !c.ImageManagerEnabledValue() {
		return ""
	}
	sockPath := strings.TrimSpace(c.ImageManagerSocket)
	if sockPath == "" {
		return DefaultImageManagerSocket
	}
	return sockPath
}

func (c RuntimeConfig) IdleEnvironmentRetentionTTLDuration() (time.Duration, error) {
	value := strings.TrimSpace(c.IdleEnvironmentRetentionTTL)
	if value == "" {
		value = DefaultIdleEnvironmentRetentionTTL
	}
	return time.ParseDuration(value)
}

func (c RuntimeConfig) EgressManagerSocketPath() string {
	value := strings.TrimSpace(c.EgressManagerSocket)
	if value == "" {
		return DefaultEgressManagerSocket
	}
	return value
}

func (c RuntimeConfig) IdleEnvironmentRetentionMaxValue() int {
	if c.IdleEnvironmentRetentionMax == nil {
		return DefaultIdleEnvironmentRetentionMax
	}
	return *c.IdleEnvironmentRetentionMax
}

func (c PluginConfig) ControlPlaneTargetValue() string {
	return strings.TrimSpace(c.ControlPlaneTarget)
}

// ValidateNodeIdentity rejects implicit identity for a control-plane-connected node.
func (c PluginConfig) ValidateNodeIdentity() error {
	if c.ControlPlaneTargetValue() == "" {
		return nil
	}
	if _, err := (workloadtls.Identity{Cluster: c.WorkloadCluster, Role: "axnoded", NodeID: c.ControlPlaneNodeID}).URI(); err != nil {
		return fmt.Errorf("explicit node identity required: %w", err)
	}
	if c.ControlPlaneEnrollmentTarget == "" || c.ControlPlaneTLSCACertValue() == "" {
		return fmt.Errorf("node enrollment endpoint and trust bundle are required")
	}
	return nil
}

func (c PluginConfig) ControlPlaneNodeTargetValue() string {
	return strings.TrimSpace(c.ControlPlaneNodeTarget)
}

func (c PluginConfig) ControlPlaneTLSCACertValue() string {
	return strings.TrimSpace(c.ControlPlaneTLSCACert)
}

func (c PluginConfig) ControlPlaneHeartbeatIntervalDuration() (time.Duration, error) {
	value := strings.TrimSpace(c.ControlPlaneHeartbeatInterval)
	if value == "" {
		value = DefaultControlPlaneHeartbeatInterval
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return time.ParseDuration(DefaultControlPlaneHeartbeatInterval)
	}
	if d > executionlease.MaxRenewalInterval {
		return 0, fmt.Errorf("control-plane heartbeat interval %s exceeds execution lease renewal maximum %s", d, executionlease.MaxRenewalInterval)
	}
	return d, nil
}

func (c PluginConfig) ControlPlaneNodeStateValue() string {
	switch strings.ToLower(strings.TrimSpace(c.ControlPlaneNodeState)) {
	case "", DefaultControlPlaneNodeState:
		return DefaultControlPlaneNodeState
	case "draining":
		return "draining"
	case "disabled":
		return "disabled"
	default:
		return DefaultControlPlaneNodeState
	}
}

func (c PluginConfig) NodeExtensionCapabilitiesValue() ([]*capabilityv1.ExtensionCapability, error) {
	if len(c.NodeExtensionCapabilities) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(c.NodeExtensionCapabilities))
	out := make([]*capabilityv1.ExtensionCapability, 0, len(c.NodeExtensionCapabilities))
	for _, configured := range c.NodeExtensionCapabilities {
		raw := &capabilityv1.ExtensionCapability{Name: configured.Name, Value: configured.Value}
		if err := capabilitycontract.ValidateExtension(raw); err != nil {
			return nil, err
		}
		capability := capabilitycontract.NormalizeExtension(raw)
		if _, duplicate := seen[capability.GetName()]; duplicate {
			return nil, fmt.Errorf("duplicate node extension capability %q", capability.GetName())
		}
		seen[capability.GetName()] = struct{}{}
		out = append(out, capability)
	}
	if len(out) > capabilitycontract.MaxExtensionCapabilities {
		return nil, fmt.Errorf("node extension capability count exceeds %d", capabilitycontract.MaxExtensionCapabilities)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GetName() != out[j].GetName() {
			return out[i].GetName() < out[j].GetName()
		}
		return out[i].GetValue() < out[j].GetValue()
	})
	return out, nil
}

func (c PluginConfig) ControlPlaneNodeLabelsValue() map[string]string {
	if len(c.ControlPlaneNodeLabels) == 0 {
		return nil
	}
	out := make(map[string]string, len(c.ControlPlaneNodeLabels))
	for key, value := range c.ControlPlaneNodeLabels {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c PluginConfig) ControlPlaneNodeResourceSourceValue() (string, error) {
	value := strings.ToLower(strings.TrimSpace(c.ControlPlaneNodeResourceSource))
	switch value {
	case "", ControlPlaneNodeResourceSourceHost:
		return ControlPlaneNodeResourceSourceHost, nil
	case ControlPlaneNodeResourceSourceKubernetes:
		return ControlPlaneNodeResourceSourceKubernetes, nil
	default:
		return "", fmt.Errorf("control_plane_node_resource_source must be either %q or %q, got %q",
			ControlPlaneNodeResourceSourceHost,
			ControlPlaneNodeResourceSourceKubernetes,
			c.ControlPlaneNodeResourceSource,
		)
	}
}

func (c ResourceConfig) ResourcePoolReconcileIntervalDuration() (time.Duration, error) {
	value := strings.TrimSpace(c.ResourcePoolReconcileInterval)
	if value == "" {
		value = DefaultResourcePoolReconcileInterval
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return time.ParseDuration(DefaultResourcePoolReconcileInterval)
	}
	return d, nil
}

func (c BPFNetConfig) SNATGCIntervalDuration() (time.Duration, error) {
	return parsePositiveDurationWithDefault(c.SNATGCInterval, DefaultBPFNetSNATGCInterval)
}

func (c BPFNetConfig) SNATTCPIdleTimeoutDuration() (time.Duration, error) {
	return parsePositiveDurationWithDefault(c.SNATTCPIdleTimeout, DefaultBPFNetSNATTCPIdleTimeout)
}

func (c BPFNetConfig) SNATTCPClosingTimeoutDuration() (time.Duration, error) {
	return parsePositiveDurationWithDefault(c.SNATTCPClosingTimeout, DefaultBPFNetSNATTCPClosingTimeout)
}

func (c BPFNetConfig) SNATDatagramIdleTimeoutDuration() (time.Duration, error) {
	return parsePositiveDurationWithDefault(c.SNATDatagramIdleTimeout, DefaultBPFNetSNATDatagramIdleTimeout)
}

func parsePositiveDurationWithDefault(value, defaultValue string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return time.ParseDuration(defaultValue)
	}
	return d, nil
}

type ResourceConfig struct {
	MaxInstanceNum int `toml:"max_instance_num" json:"maxInstanceNum"`
	// MemorySystemReserveBytes covers axnoded, lifecycle monitors, and all
	// node-local control daemons outside sandbox cgroups. Production must set an
	// explicit positive value; disabled_dev may explicitly use zero.
	MemorySystemReserveBytes int64 `toml:"memory_system_reserve_bytes" json:"memorySystemReserveBytes"`

	// CgroupRootName is the single sandbox child name created beneath the
	// process's delegated cgroup-v2 root. It is not a host-absolute path.
	CgroupRootName string `toml:"cgroup_root_name" json:"cgroupRootName"`
	// CgroupCacheSize is the target number of never-assigned warm cgroups.
	// Zero disables prewarming, not cgroup enforcement or on-demand creation.
	CgroupCacheSize int `toml:"cgroup_cache_size" json:"cgroupCacheSize"`
	// InterfaceCacheSize is the size of interface cache. Default is same as max_instance_num.
	InterfaceCacheSize int `toml:"interface_cache_size" json:"interfaceCacheSize"`
	// ResourcePoolReconcileInterval controls how frequently axnoded
	// reconciles the cgroup/interface warm pools toward their idle target.
	ResourcePoolReconcileInterval string `toml:"resource_pool_reconcile_interval" json:"resourcePoolReconcileInterval"`
}

func (c ResourceConfig) CgroupRootNameValue() (string, error) {
	name := strings.TrimSpace(c.CgroupRootName)
	if name == "" {
		name = DefaultCgroupRoot
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\\`) || name == "internal" || name == "workload" || name == "conformance" {
		return "", fmt.Errorf("cgroup_root_name %q must be a non-reserved single child name", name)
	}
	return name, nil
}

// NetworkConfig contains network-related configuration for axnoded.
type NetworkConfig struct {
	IPRange string `toml:"ip_range" json:"ipRange"`

	// NatBackend selects the sandbox egress NAT implementation.
	// The generic build supports "iptables" and "ebpf".
	NatBackend string `toml:"nat_backend" json:"natBackend"`

	BPFNet BPFNetConfig `toml:"ebpf" json:"ebpf"`
}

type BPFNetConfig struct {
	UplinkDevices []string `toml:"uplink_devices" json:"uplinkDevices"`

	PinPath string `toml:"pin_path" json:"pinPath"`

	SNATMapSize int `toml:"snat_map_size" json:"snatMapSize"`

	SNATGCInterval string `toml:"snat_gc_interval" json:"snatGcInterval"`

	SNATTCPIdleTimeout string `toml:"snat_tcp_idle_timeout" json:"snatTcpIdleTimeout"`

	SNATTCPClosingTimeout string `toml:"snat_tcp_closing_timeout" json:"snatTcpClosingTimeout"`

	SNATDatagramIdleTimeout string `toml:"snat_datagram_idle_timeout" json:"snatDatagramIdleTimeout"`

	NativeRoutingCIDRs []string `toml:"native_routing_cidrs" json:"nativeRoutingCidrs"`
}

// Normalized returns the exact network configuration consumed by the runtime
// and capability evidence. It canonicalizes semantically unordered sets and
// effective duration values so evidence identity changes only when dataplane
// behavior changes.
func (c NetworkConfig) Normalized() (NetworkConfig, error) {
	c.NatBackend = strings.ToLower(strings.TrimSpace(c.NatBackend))
	switch c.NatBackend {
	case NatBackendIptables, NatBackendEBPF:
	default:
		return NetworkConfig{}, fmt.Errorf("unsupported nat_backend %q", c.NatBackend)
	}

	ipRange, err := netip.ParsePrefix(strings.TrimSpace(c.IPRange))
	if err != nil || (!ipRange.Addr().Is4() && !ipRange.Addr().Is6()) {
		return NetworkConfig{}, fmt.Errorf("network ip_range must be a valid IPv4 or IPv6 prefix: %q", c.IPRange)
	}
	c.IPRange = ipRange.String()
	if c.NatBackend == NatBackendEBPF && ipRange.Addr().Is6() {
		return NetworkConfig{}, fmt.Errorf("ebpf network backend supports IPv4 only; select the iptables backend for an IPv6 sandbox range")
	}

	c.BPFNet.PinPath = filepath.Clean(strings.TrimSpace(c.BPFNet.PinPath))
	if c.NatBackend == NatBackendEBPF {
		if !filepath.IsAbs(c.BPFNet.PinPath) {
			return NetworkConfig{}, fmt.Errorf("ebpf pin_path must be absolute: %q", c.BPFNet.PinPath)
		}
		if c.BPFNet.SNATMapSize <= 0 {
			return NetworkConfig{}, fmt.Errorf("ebpf snat_map_size must be positive")
		}
	}

	if c.BPFNet.UplinkDevices, err = normalizeUniqueStrings("ebpf uplink_devices", c.BPFNet.UplinkDevices); err != nil {
		return NetworkConfig{}, err
	}
	if c.BPFNet.NativeRoutingCIDRs, err = normalizeCIDRs(c.BPFNet.NativeRoutingCIDRs); err != nil {
		return NetworkConfig{}, err
	}
	if duration, parseErr := c.BPFNet.SNATGCIntervalDuration(); parseErr != nil {
		return NetworkConfig{}, fmt.Errorf("ebpf snat_gc_interval: %w", parseErr)
	} else {
		c.BPFNet.SNATGCInterval = duration.String()
	}
	if duration, parseErr := c.BPFNet.SNATTCPIdleTimeoutDuration(); parseErr != nil {
		return NetworkConfig{}, fmt.Errorf("ebpf snat_tcp_idle_timeout: %w", parseErr)
	} else {
		c.BPFNet.SNATTCPIdleTimeout = duration.String()
	}
	if duration, parseErr := c.BPFNet.SNATTCPClosingTimeoutDuration(); parseErr != nil {
		return NetworkConfig{}, fmt.Errorf("ebpf snat_tcp_closing_timeout: %w", parseErr)
	} else {
		c.BPFNet.SNATTCPClosingTimeout = duration.String()
	}
	if duration, parseErr := c.BPFNet.SNATDatagramIdleTimeoutDuration(); parseErr != nil {
		return NetworkConfig{}, fmt.Errorf("ebpf snat_datagram_idle_timeout: %w", parseErr)
	} else {
		c.BPFNet.SNATDatagramIdleTimeout = duration.String()
	}
	return c, nil
}

func normalizeUniqueStrings(field string, values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s contains an empty value", field)
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("%s contains duplicate value %q", field, value)
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeCIDRs(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("ebpf native_routing_cidrs contains invalid prefix %q", value)
		}
		canonical := prefix.Masked().String()
		if _, duplicate := seen[canonical]; duplicate {
			return nil, fmt.Errorf("ebpf native_routing_cidrs contains duplicate prefix %q", canonical)
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	sort.Strings(out)
	return out, nil
}

// DefaultConfig returns default configurations of cri plugin.
func DefaultConfig() Config {
	defaultIdleEnvironmentRetentionMax := DefaultIdleEnvironmentRetentionMax
	return Config{
		PluginConfig: PluginConfig{
			NetworkConfig: NetworkConfig{
				NatBackend: "iptables",
				IPRange:    DefaultIPRange,
				BPFNet: BPFNetConfig{
					PinPath:                 DefaultBPFNetPinPath,
					SNATMapSize:             DefaultBPFNetSNATMapSize,
					SNATGCInterval:          DefaultBPFNetSNATGCInterval,
					SNATTCPIdleTimeout:      DefaultBPFNetSNATTCPIdleTimeout,
					SNATTCPClosingTimeout:   DefaultBPFNetSNATTCPClosingTimeout,
					SNATDatagramIdleTimeout: DefaultBPFNetSNATDatagramIdleTimeout,
				},
			},
			RuntimeConfig: RuntimeConfig{
				Runsc: RuntimeInstanceConfig{
					Binary:   DefaultRunscBinary,
					BaseSpec: "/etc/axnoded/runsc-config.json",
					Options: RuntimeOptions{
						AllowSUID: boolPtr(true),
					},
				},
				CgroupEnforcement:                 CgroupEnforcementRequired,
				ImageLibDir:                       DefaultImageLibDir,
				ImageManagerEnabled:               boolPtr(true),
				ImageManagerSocket:                DefaultImageManagerSocket,
				EgressManagerSocket:               DefaultEgressManagerSocket,
				IdleEnvironmentRetentionTTL:       DefaultIdleEnvironmentRetentionTTL,
				IdleEnvironmentRetentionMax:       &defaultIdleEnvironmentRetentionMax,
				FilestoreMode:                     FilestoreModeExisting,
				EphemeralStorageDefaultLimitBytes: 256 << 20,
			},
			ResourceConfig: ResourceConfig{
				MaxInstanceNum:                DefaultMaxContainerNum,
				CgroupRootName:                DefaultCgroupRoot,
				CgroupCacheSize:               DefaultMaxContainerNum,
				InterfaceCacheSize:            DefaultMaxContainerNum,
				ResourcePoolReconcileInterval: DefaultResourcePoolReconcileInterval,
			},
			ControlPlaneHeartbeatInterval: DefaultControlPlaneHeartbeatInterval,
			ControlPlaneNodeState:         DefaultControlPlaneNodeState,
		},
		RootDir:  DefaultContainerRootDir,
		StoreDir: DefaultStoreDir,
	}
}

func boolPtr(v bool) *bool {
	return &v
}
