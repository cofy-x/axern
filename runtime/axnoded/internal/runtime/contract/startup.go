package contract

import (
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

type StartupPhase string
type StartupStep string

const (
	StartupPhaseLangRuntimeLookup StartupPhase = "langruntime_lookup"
	StartupPhaseRootfsPrepare     StartupPhase = "rootfs_prepare"
	StartupPhaseResourceAllocate  StartupPhase = "resource_allocate"
	StartupPhaseRuntimeBundle     StartupPhase = "runtime_bundle_prepare"
	StartupPhaseRuntimeLaunch     StartupPhase = "runtime_launch"
	StartupPhaseNetworkActivate   StartupPhase = "network_activate"
	StartupResultOK               string       = "ok"
	StartupResultError            string       = "error"
	StartupClassCold              string       = "cold"
	StartupClassWarm              string       = "warm"
	StartupRootfsTypeLocal        string       = "local"
	StartupRootfsTypeImage        string       = "image"
	StartupRootfsTypeS3           string       = "s3"
	StartupRootfsTypeUnknown      string       = "unknown"
)

const (
	StartupStepRootfsResolve         StartupStep = "rootfs_resolve"
	StartupStepRootfsCacheLookup     StartupStep = "rootfs_cache_lookup"
	StartupStepRootfsWait            StartupStep = "rootfs_wait"
	StartupStepRootfsMount           StartupStep = "rootfs_mount"
	StartupStepRootfsActiveRef       StartupStep = "rootfs_active_ref"
	StartupStepRuntimeCgroupPrepare  StartupStep = "runtime_cgroup_prepare"
	StartupStepRuntimeBundleMaterial StartupStep = "runtime_bundle_materialize"
	StartupStepRootfsViewPrepare     StartupStep = "rootfs_view_prepare"
	StartupStepRootfsViewApply       StartupStep = "rootfs_view_apply"
	StartupStepMountTargetsPrepare   StartupStep = "mount_targets_prepare"
	StartupStepRuntimeOverlayArgs    StartupStep = "runtime_overlay_args"
	StartupStepRuntimeStart          StartupStep = "runtime_start"
	StartupStepRuntimeWaitStart      StartupStep = "runtime_wait_start"
	StartupStepRuntimeExitMonitor    StartupStep = "runtime_exit_monitor"
	StartupStepSandboxdWaitReady     StartupStep = "sandboxd_wait_ready"
	StartupStepRuntimeEnforcement    StartupStep = "runtime_enforcement_verify"
	StartupStepRuntimeRestore        StartupStep = "runtime_restore"
)

type StartupPhaseRecorder interface {
	RecordStartupPhase(phase StartupPhase, duration time.Duration)
}

type StartupStepRecorder interface {
	RecordStartupStep(phase StartupPhase, step StartupStep, duration time.Duration)
}

func RootfsTypeLabel(rootfs *runtimeapi.RootfsConfig) string {
	if rootfs == nil {
		return StartupRootfsTypeUnknown
	}

	switch rootfs.GetType() {
	case runtimeapi.RootfsSrcType_LOCAL:
		return StartupRootfsTypeLocal
	case runtimeapi.RootfsSrcType_IMAGE:
		return StartupRootfsTypeImage
	case runtimeapi.RootfsSrcType_S3:
		return StartupRootfsTypeS3
	default:
		return StartupRootfsTypeUnknown
	}
}
