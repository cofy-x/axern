package config

import "time"

const (
	StopTimeout = 10 * time.Second

	DefaultSocketAddress = "/run/axnoded/axnoded.sock"
	DefaultRootDir       = "/var/lib/axnoded"
	DefaultTimeout       = time.Second * 10

	DefaultContainerRootDir = "/var/lib/axnoded/root"
	DefaultStoreDir         = "/var/lib/axnoded/store"

	DefaultLogDir                        = "/var/log/axnoded"
	DefaultImageLibDir                   = "/var/lib/axnoded/rootfs"
	DefaultImageManagerSocket            = "/var/run/imagemgr.sock"
	DefaultVolumeManagerSocket           = "/run/volumed/volumed.sock"
	DefaultEgressManagerSocket           = "/run/egressd/egressd.sock"
	DefaultIdleRuntimeRetentionTTL       = "5m"
	DefaultIdleRuntimeRetentionMax       = 8
	DefaultResourcePoolReconcileInterval = "1s"
	DefaultControlPlaneHeartbeatInterval = "5s"
	DefaultControlPlaneNodeState         = "ready"

	DefaultHttpAddress = ":23001"

	DefaultMaxContainerNum  = 1000
	DefaultMaxCacheLimitNum = 800

	DefaultCgroupRoot = "sandbox"
	// RuntimeConformanceMemoryLimitBytes is the hard limit exercised inside the
	// certification sandbox. RuntimeConformanceMemoryMaxBytes is the larger
	// aggregate ceiling for that sandbox plus runtime-specific monitor and
	// control processes. Conflating the two lets a runsc certification OOM its
	// own control plane instead of proving the workload boundary.
	RuntimeConformanceMemoryLimitBytes int64 = 256 << 20
	RuntimeConformanceMemoryMaxBytes   int64 = 512 << 20

	DefaultIPRange = "172.17.0.1/16"

	DefaultRunscBinary         = "/usr/local/bin/runsc"
	DefaultRuntimeRunnerBinary = "/usr/local/libexec/axnoded/axnoded-runtime-runner"

	DefaultBPFNetPinPath                 = "/sys/fs/bpf/axern/bpfnet"
	DefaultBPFNetMapSize                 = 16384
	DefaultBPFNetSNATMapSize             = 262144
	DefaultBPFNetSNATGCInterval          = "1s"
	DefaultBPFNetSNATTCPIdleTimeout      = "5m"
	DefaultBPFNetSNATTCPClosingTimeout   = "2s"
	DefaultBPFNetSNATDatagramIdleTimeout = "10s"
)
