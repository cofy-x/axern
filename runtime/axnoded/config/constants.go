package config

import (
	"time"
)

// RuntimeNameRunsc is the name of runsc runtime
const RuntimeNameRunsc = "runsc"

// RuntimeNameRunc is the name of runc runtime.
const RuntimeNameRunc = "runc"

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
	SandboxEnvKey          = "RUNTIME_ENV_ID"
	SandboxFunctionNameKey = "RUNTIME_FUNCTION_NAME"

	SandboxContainerOverlayfsLowerDirLabel  = "io.sandbox.container.overlayfs.lowerDir"
	SandboxContainerOverlayfsTargetDirLabel = "io.sandbox.container.overlayfs.targetDir"

	OverlayUpperDirName = "overlay-upper"
	OverlayWorkDirName  = "overlay-work"
)

const (
	NetAcRule = `{
		"Version": "",
		"AppName": "",
		"StartTime": "0001-01-01T00:00:00Z",
		"RuleSetName": "",
		"DnsRuleSet": null,
		"IngressRuleSet": null,
		"EgressRuleSet": [
		  {
			"RuleName": "function gateway whitelist",
			"ip_version": 4,
			"dst_ports": [
			  {
				"protocol": "tcp",
				"first": 8081,
				"last": 8081
			  }
			],
			"dst_net": [
			  "11.166.47.237/32"
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
			"RuleName": "function instance blacklist",
			"ip_version": 4,
			"dst_ports": [
			  {
				"protocol": "all",
				"first": -1,
				"last": -1
			  }
			],
			"dst_net": [
			  "172.17.0.1/16"
			],
			"dst_domain": "",
			"Log": true,
			"Action": "drop",
			"Priority": 2,
			"FuseEnable": false,
			"FuseConfig": {
			  "TimeDuration": 0,
			  "Threshold": 0,
			  "Version": ""
			}
		  },
		  {
			"RuleName": "function vpc blacklist",
			"ip_version": 4,
			"dst_ports": [
			  {
				"protocol": "all",
				"first": -1,
				"last": -1
			  }
			],
			"dst_net": [
			  "6.0.0.0/8"
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

	NetAcBlockAll = `{
		"Version": "",
		"AppName": "",
		"StartTime": "0001-01-01T00:00:00Z",
		"RuleSetName": "",
		"DnsRuleSet": null,
		"IngressRuleSet": null,
		"EgressRuleSet": [
		  {
			"RuleName": "function proxy whitelist",
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
