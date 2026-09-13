package contract

import (
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

type HandlerOptions struct {
	TraceID     string
	SpanID      string
	ContainerID string

	StartupPhaseRecorder StartupPhaseRecorder

	ForceDelete bool

	CgroupPath                 string
	RuntimeCgroupPath          string
	MemoryLimitBytes           int64
	EphemeralStorageLimitBytes int64
	EnforcementManifest        *apipb.AllocationEnforcementManifest
	AllocatedResources         map[resourcemanager.ResourceName]string
	RootfsType                 string

	BundleTemplateCarrier runtimeoci.TemplateCarrier
	BundleTemplateSource  *runtimeoci.TemplateOptions

	ResourceAnnotations map[string]string
	ExecutionProfile    *runtimeoci.ExecutionProfile
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
