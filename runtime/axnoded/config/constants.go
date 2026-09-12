package config

import (
	"time"
)

// RuntimeNameRunsc is the name of runsc runtime
const RuntimeNameRunsc = "runsc"

const (
	FilestoreModeExisting        = "existing"
	FilestoreModeLoopbackDev     = "loopback_dev"
	CgroupEnforcementRequired    = "required"
	CgroupEnforcementDisabledDev = "disabled_dev"
)

// Sandbox service related constants.
const (
	UnknownVersion = "unknown"

	SandboxServiceName = "sandbox"
)

const (
	SandboxContainerPrefix = "axctl"
	ContainerSpecFile      = "config.json"
	ContainerMetaFile      = "meta.pb"
	ContainerStatusFile    = "status"
)

const (
	RecycleBin = "_recycle"

	CheckpointSuffix = "_checkpoint.img"
	NotifyFile       = "/ready.signal"
)

const (
	// HouseKeepingMaxCostTime is the max cost time of housekeeping.
	// If the cost time is larger than this value,
	// a warning log will be printed and the server will set self to unhealthy.
	HouseKeepingMaxCostTime = time.Second * 5

	// LifeCycleAliveValidInterval is the valid interval of alive check before terrible.
	LifeCycleAliveValidInterval = time.Second * 5
)

// Bucket key store bucket
const (
	// CgroupBucket stores the active cgroup resource snapshot.
	CgroupBucket = "cgroups"
	// BridgeIPBucket stores the active network interface snapshot.
	BridgeIPBucket = "network_interfaces"
	// AllocationStateBucket stores one durable record per active allocation.
	AllocationStateBucket = "allocations"
	// AllocationStatusOutboxBucket stores terminal allocation observations until
	// controld has acknowledged the corresponding status-report RPC. Resource
	// cleanup may remove the container checkpoint before that acknowledgement,
	// so the reporting barrier requires its own durable ownership record.
	AllocationStatusOutboxBucket = "allocation_status_outbox"
	// DNATRulesBucket stores the active DNAT rule snapshot.
	DNATRulesBucket = "dnat_rules"
	// MemoryObservationSequenceBucket stores the reserved high watermark for
	// allocation memory observation revisions. Sequence blocks are persisted
	// before use so an axnoded restart can skip values but never reuse them.
	MemoryObservationSequenceBucket = "memory_observation_sequence"
)

const (
	ControlPlaneNodeResourceSourceHost       = "host"
	ControlPlaneNodeResourceSourceKubernetes = "kubernetes"
)

// Network related constants.
const (
	HostVethPrefix = "hv."
	PeerVethPrefix = "pv."

	NatBackendIptables = "iptables"
	NatBackendEBPF     = "ebpf"
)

const (
	SandboxEnvKey = "RUNTIME_ENV_ID"

	SandboxContainerOverlayfsLowerDirLabel  = "io.sandbox.container.overlayfs.lowerDir"
	SandboxContainerOverlayfsTargetDirLabel = "io.sandbox.container.overlayfs.targetDir"

	OverlayUpperDirName = "overlay-upper"
	OverlayWorkDirName  = "overlay-work"
)

const (
	NetAcBlockAll = `{
		"Version": "",
		"AppName": "",
		"StartTime": "0001-01-01T00:00:00Z",
		"RuleSetName": "",
		"DnsRuleSet": null,
		"IngressRuleSet": null,
		"EgressRuleSet": [
		  {
			"RuleName": "sandbox proxy whitelist",
			"ip_version": 4,
			"dst_ports": [
			  {
				"protocol": "tcp",
				"first": 22722,
				"last": 22722
			  }
			],
			"dst_net": [
			  "172.17.0.1/32"
			],
			"dst_domain": "",
			"Log": true,
			"Action": "pass",
			"Priority": 3,
			"FuseEnable": false,
			"FuseConfig": {
			  "TimeDuration": 0,
			  "Threshold": 0,
			  "Version": ""
			}
		  },
		  {
			"RuleName": "all blacklist",
			"ip_version": 4,
			"dst_ports": [
			  {
				"protocol": "all",
				"first": -1,
				"last": -1
			  }
			],
			"dst_net": [
			  "0.0.0.0/0"
			],
			"dst_domain": "",
			"Log": true,
			"Action": "drop",
			"Priority": 1,
			"FuseEnable": false,
			"FuseConfig": {
			  "TimeDuration": 0,
			  "Threshold": 0,
			  "Version": ""
			}
		  }
		]
	  }`
)
