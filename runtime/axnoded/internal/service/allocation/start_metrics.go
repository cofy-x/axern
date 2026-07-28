package allocation

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
)

type LangRuntimePrepareSummary struct {
	RuntimeReused         bool
	RootfsType            string
	LangRuntimeLookupTime time.Duration
	RootfsPrepareTime     time.Duration
	Steps                 []StartupStepSample
}

func (s LangRuntimePrepareSummary) StartClass() string {
	if s.RuntimeReused {
		return contract.StartupClassWarm
	}
	return contract.StartupClassCold
}

type StartupStepSample struct {
	Phase    contract.StartupPhase
	Step     contract.StartupStep
	Duration time.Duration
}

type StartMetricSink interface {
	RecordStartDuration(startClass, runtime, rootfsType, result string, duration time.Duration)
	RecordStartPhaseDuration(phase contract.StartupPhase, startClass, runtime, rootfsType, result string, duration time.Duration)
	RecordStartStepDuration(phase contract.StartupPhase, step contract.StartupStep, startClass, runtime, rootfsType, result string, duration time.Duration)
	RecordStartResult(startClass, runtime, rootfsType, result string)
}

type DefaultStartMetricSink struct{}

func (DefaultStartMetricSink) RecordStartDuration(startClass, runtime, rootfsType, result string, duration time.Duration) {
	metrics.RecordStartDuration(startClass, runtime, rootfsType, result, duration.Seconds())
}

func (DefaultStartMetricSink) RecordStartPhaseDuration(phase contract.StartupPhase, startClass, runtime, rootfsType, result string, duration time.Duration) {
	metrics.RecordStartPhaseDuration(string(phase), startClass, runtime, rootfsType, result, duration.Seconds())
}

func (DefaultStartMetricSink) RecordStartStepDuration(phase contract.StartupPhase, step contract.StartupStep, startClass, runtime, rootfsType, result string, duration time.Duration) {
	metrics.RecordStartStepDuration(string(phase), string(step), startClass, runtime, rootfsType, result, duration.Seconds())
}

func (DefaultStartMetricSink) RecordStartResult(startClass, runtime, rootfsType, result string) {
	metrics.RecordStartResult(startClass, runtime, rootfsType, result)
}

type startMetricsRecorder struct {
	sink       StartMetricSink
	runtime    string
	rootfsType string
	startClass string
	startedAt  time.Time
	phases     []phaseSample
	steps      []StartupStepSample
}

type phaseSample struct {
	phase    contract.StartupPhase
	duration time.Duration
}

func NewStartMetricsRecorder(sink StartMetricSink, runtimeName, rootfsType string) *startMetricsRecorder {
	if sink == nil {
		sink = DefaultStartMetricSink{}
	}

	return &startMetricsRecorder{
		sink:       sink,
		runtime:    normalizeStartMetricValue(runtimeName, "unknown"),
		rootfsType: normalizeStartMetricValue(rootfsType, contract.StartupRootfsTypeUnknown),
		startClass: contract.StartupClassCold,
		startedAt:  time.Now(),
		phases:     make([]phaseSample, 0, 8),
		steps:      make([]StartupStepSample, 0, 16),
	}
}

func (r *startMetricsRecorder) SetStartClass(startClass string) {
	r.startClass = normalizeStartMetricValue(startClass, contract.StartupClassCold)
}

func (r *startMetricsRecorder) SetRootfsType(rootfsType string) {
	r.rootfsType = normalizeStartMetricValue(rootfsType, contract.StartupRootfsTypeUnknown)
}

func (r *startMetricsRecorder) RecordStartupPhase(phase contract.StartupPhase, duration time.Duration) {
	if r == nil || duration <= 0 {
		return
	}
	r.phases = append(r.phases, phaseSample{phase: phase, duration: duration})
}

func (r *startMetricsRecorder) RecordStartupStep(phase contract.StartupPhase, step contract.StartupStep, duration time.Duration) {
	if r == nil || duration <= 0 || phase == "" || step == "" {
		return
	}
	r.steps = append(r.steps, StartupStepSample{Phase: phase, Step: step, Duration: duration})
}

func (r *startMetricsRecorder) Finish(result string) {
	if r == nil || r.sink == nil {
		return
	}

	result = normalizeStartMetricValue(result, contract.StartupResultError)
	r.sink.RecordStartResult(r.startClass, r.runtime, r.rootfsType, result)
	for _, sample := range r.phases {
		r.sink.RecordStartPhaseDuration(sample.phase, r.startClass, r.runtime, r.rootfsType, result, sample.duration)
	}
	for _, sample := range r.steps {
		r.sink.RecordStartStepDuration(sample.Phase, sample.Step, r.startClass, r.runtime, r.rootfsType, result, sample.Duration)
	}
	r.sink.RecordStartDuration(r.startClass, r.runtime, r.rootfsType, result, time.Since(r.startedAt))
}

func RootfsTypeFromRuntimeTemplate(fr *runtimeapi.RuntimeTemplate) string {
	if fr == nil {
		return contract.StartupRootfsTypeUnknown
	}
	return contract.RootfsTypeLabel(fr.GetRootfs())
}

func normalizeStartMetricValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
