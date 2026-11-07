package contract

import (
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type HandlerOptions struct {
	TraceID     string
	SpanID      string
	ContainerID string

	StartupPhaseRecorder StartupPhaseRecorder

	ForceDelete bool
	// RuntimeConformanceCleanup is set only after the service verifies the
	// exact container checkpoint against a durable RUNTIME_CONFORMANCE cgroup
	// lease. It permits no-signal cleanup of an already-dead legacy probe that
	// lacks a memory-domain identity; it never permits start, resume, or kill.
	RuntimeConformanceCleanup bool

	CgroupPath                 string
	RuntimeCgroupPath          string
	MemoryLimitBytes           int64
	EphemeralStorageLimitBytes int64
	EnforcementManifest        *apipb.AllocationEnforcementManifest
	AllocatedResources         map[resourcemanager.ResourceName]string
	RootfsType                 string

	BundleTemplateCarrier runtimeoci.TemplateCarrier
	BundleTemplateSource  *runtimeoci.TemplateOptions

	NetworkNamespacePath string
	NetworkMode          commonv1.NetworkMode
	SandboxIP            string
	ExecutionProfile     *runtimeoci.ExecutionProfile
}

func (o HandlerOptions) RecordStartupPhase(phase StartupPhase, duration time.Duration) {
	if o.StartupPhaseRecorder == nil {
		return
	}
	o.StartupPhaseRecorder.RecordStartupPhase(phase, duration)
}

func (o HandlerOptions) RecordStartupStep(phase StartupPhase, step StartupStep, duration time.Duration) {
	if o.StartupPhaseRecorder == nil {
		return
	}
	recorder, ok := o.StartupPhaseRecorder.(StartupStepRecorder)
	if !ok {
		return
	}
	recorder.RecordStartupStep(phase, step, duration)
}
