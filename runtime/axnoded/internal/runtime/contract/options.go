package contract

import (
	"time"

	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

type HandlerOptions struct {
	TraceID     string
	SpanID      string
	ContainerID string
	// ContainerLabels carries persisted metadata labels for runtime paths that
	// validate sandboxd create-time readiness and baseline capabilities before
	// dispatching to the daemon socket derived from ContainerID.
	ContainerLabels map[string]string

	StartupPhaseRecorder StartupPhaseRecorder

	ForceDelete  bool
	CleanRootDir string

	CgroupPath         string
	RuntimeCgroupPath  string
	AllocatedResources map[resourcemanager.ResourceName]string
	RootfsType         string

	BundleTemplateCarrier runtimeoci.TemplateCarrier
	BundleTemplateSource  *runtimeoci.TemplateOptions

	AdditionalAnnotations map[string]string
	ExecutionProfile      *runtimeoci.ExecutionProfile
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
