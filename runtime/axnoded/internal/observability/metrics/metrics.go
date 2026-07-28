package metrics

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

const (
	TypeCounter     = "counter"
	TypeGauge       = "gauge"
	TypeHistogram   = "histogram"
	SnapshotVersion = "v1"
)

// The debug recorder is a bounded, process-local mirror for benchmark and
// verification snapshots. Production metrics are exported only through OTEL.
const (
	maxDebugSeries           = 2048
	maxDebugHistogramSamples = 8192
)

const (
	MetricSandboxActionDuration                  = "axern.axnoded_sandbox_action_duration_seconds"
	MetricSandboxActionTotal                     = "axern.axnoded_sandbox_action_total"
	MetricSandboxResourceCurrent                 = "axern.axnoded_sandbox_resource_current"
	MetricRuntimeCallTotal                       = "axern.axnoded_runtime_call_total"
	MetricGCQueueCurrent                         = "axern.axnoded_gc_queue_current"
	MetricStartupTotal                           = "axern.axnoded_startup_total"
	MetricStartupDuration                        = "axern.axnoded_startup_duration_seconds"
	MetricStartupPhaseDuration                   = "axern.axnoded_startup_phase_duration_seconds"
	MetricStartupStepDuration                    = "axern.axnoded_startup_step_duration_seconds"
	MetricLifecycleStageDuration                 = "axern.axnoded_lifecycle_stage_duration_seconds"
	MetricAllocationDeleteStageDuration          = "axern.axnoded_allocation_delete_stage_duration_seconds"
	MetricHTTPProxyStageDuration                 = "axern.axnoded_http_proxy_stage_duration_seconds"
	MetricExecutionLeaseVisibilityDuration       = "axern.axnoded_execution_lease_visibility_duration_seconds"
	MetricRetainedRuntimeCurrent                 = "axern.axnoded_retained_runtime_current"
	MetricRetainedRootfsCurrent                  = "axern.axnoded_retained_rootfs_current"
	MetricRetentionReuseTotal                    = "axern.axnoded_retention_reuse_total"
	MetricRetentionEvictionTotal                 = "axern.axnoded_retention_eviction_total"
	MetricResourcePoolIdleCurrent                = "axern.axnoded_resource_pool_idle_current"
	MetricResourcePoolUsingCurrent               = "axern.axnoded_resource_pool_using_current"
	MetricResourcePoolTargetCurrent              = "axern.axnoded_resource_pool_target_current"
	MetricResourcePoolAllocateTotal              = "axern.axnoded_resource_pool_allocate_total"
	MetricResourceAllocateStageDuration          = "axern.axnoded_resource_allocate_stage_duration_seconds"
	MetricResourceAllocateObservationDropped     = "axern.axnoded_resource_allocate_observation_dropped_total"
	MetricResourcePoolRefillTotal                = "axern.axnoded_resource_pool_refill_total"
	MetricResourcePoolRefillDuration             = "axern.axnoded_resource_pool_refill_duration_seconds"
	MetricBundleTemplateTotal                    = "axern.axnoded_bundle_template_total"
	MetricBundleMaterializeDuration              = "axern.axnoded_bundle_materialize_duration_seconds"
	MetricExecutionEnvelopeTotal                 = "axern.axnoded_execution_envelope_total"
	MetricExecutionEnvelopePrepare               = "axern.axnoded_execution_envelope_prepare_duration_seconds"
	MetricExecutionEnvelopeActivate              = "axern.axnoded_execution_envelope_activate_duration_seconds"
	MetricExecutionEnvelopeCurrent               = "axern.axnoded_execution_envelope_current"
	MetricRuntimeWaitGraceTotal                  = "axern.axnoded_runtime_wait_grace_total"
	MetricControlPlaneRPCTotal                   = "axern.axnoded_control_plane_rpc_total"
	MetricControlPlaneRPCDuration                = "axern.axnoded_control_plane_rpc_duration_seconds"
	MetricAllocationStatusQueueTotal             = "axern.axnoded_allocation_status_queue_total"
	MetricAllocationStatusQueueCurrent           = "axern.axnoded_allocation_status_queue_current"
	MetricAllocationStatusQueueWait              = "axern.axnoded_allocation_status_queue_wait_duration_seconds"
	MetricAllocationStatusBatchTotal             = "axern.axnoded_allocation_status_batch_total"
	MetricAllocationStatusBatchObservationsTotal = "axern.axnoded_allocation_status_batch_observations_total"
	MetricAllocationStatusOldestPendingAge       = "axern.axnoded_allocation_status_oldest_pending_age_seconds"
	MetricAllocationStatusConsecutiveFailures    = "axern.axnoded_allocation_status_consecutive_failures"
	MetricAllocationStatusRetryDelay             = "axern.axnoded_allocation_status_retry_delay_seconds"
	MetricReadinessWaitDuration                  = "axern.axnoded_readiness_wait_duration_seconds"
	MetricProbeAttemptDuration                   = "axern.axnoded_probe_attempt_duration_seconds"
	MetricVolumeOperationTotal                   = "axern.axnoded_volume_operation_total"
	MetricVolumeReconcileCurrent                 = "axern.axnoded_volume_reconcile_current"
)

const (
	MetricReadinessProbeStageDuration = "axern.axnoded_readiness_probe_stage_duration_seconds"
	MetricNetworkNeighborResetTotal   = "axern.axnoded_network_neighbor_reset_total"
)

const (
	descSandboxActionDuration                  = "Axnoded sandbox API action duration."
	descSandboxActionTotal                     = "Axnoded sandbox API action results."
	descSandboxResourceCurrent                 = "Axnoded sandbox resource current value."
	descRuntimeCallTotal                       = "Axnoded runtime call results."
	descGCQueueCurrent                         = "Axnoded GC queue current length."
	descStartupTotal                           = "Axnoded sandbox start requests."
	descStartupDuration                        = "Axnoded sandbox start duration."
	descStartupPhaseDuration                   = "Axnoded sandbox start phase duration."
	descStartupStepDuration                    = "Axnoded sandbox start step duration."
	descLifecycleStageDuration                 = "Axnoded node lifecycle RPC handling stage duration."
	descAllocationDeleteStageDuration          = "Axnoded allocation delete stage duration."
	descHTTPProxyStageDuration                 = "Axnoded HTTP proxy stage duration."
	descExecutionLeaseVisibilityDuration       = "Axnoded execution lease cache visibility duration."
	descRetainedRuntimeCurrent                 = "Axnoded retained idle runtime count."
	descRetainedRootfsCurrent                  = "Axnoded retained rootfs count."
	descRetentionReuseTotal                    = "Axnoded retention reuse events."
	descRetentionEvictionTotal                 = "Axnoded retention eviction events."
	descResourcePoolIdleCurrent                = "Axnoded resource pool idle count."
	descResourcePoolUsingCurrent               = "Axnoded resource pool in-use count."
	descResourcePoolTargetCurrent              = "Axnoded resource pool configured idle target."
	descResourcePoolAllocateTotal              = "Axnoded resource pool allocation results."
	descResourceAllocateStageDuration          = "Axnoded resource allocation stage duration."
	descResourceAllocateObservationDropped     = "Axnoded resource allocation observations dropped by the bounded metrics queue."
	descResourcePoolRefillTotal                = "Axnoded resource pool refill attempts."
	descResourcePoolRefillDuration             = "Axnoded resource pool refill duration."
	descBundleTemplateTotal                    = "Axnoded bundle template results."
	descBundleMaterializeDuration              = "Axnoded bundle materialization duration."
	descExecutionEnvelopeTotal                 = "Axnoded execution-envelope results."
	descExecutionEnvelopePrepare               = "Axnoded execution-envelope prepare duration."
	descExecutionEnvelopeActivate              = "Axnoded execution-envelope activation duration."
	descExecutionEnvelopeCurrent               = "Axnoded execution-envelope lifecycle current state."
	descRuntimeWaitGraceTotal                  = "Axnoded runtime wait grace-path resolutions."
	descControlPlaneRPCTotal                   = "Axnoded control-plane reporter RPC attempts."
	descControlPlaneRPCDuration                = "Axnoded control-plane reporter RPC duration."
	descAllocationStatusQueueTotal             = "Axnoded allocation status queue events."
	descAllocationStatusQueueCurrent           = "Axnoded pending allocation status observations."
	descAllocationStatusQueueWait              = "Axnoded allocation status observation queue wait duration."
	descAllocationStatusBatchTotal             = "Axnoded allocation status batches by result."
	descAllocationStatusBatchObservationsTotal = "Axnoded allocation status observations sent in batches by result."
	descAllocationStatusOldestPendingAge       = "Axnoded oldest pending allocation status observation age."
	descAllocationStatusConsecutiveFailures    = "Axnoded consecutive allocation status batch failures."
	descAllocationStatusRetryDelay             = "Axnoded current allocation status retry delay."
	descReadinessWaitDuration                  = "Axnoded readiness wait duration."
	descProbeAttemptDuration                   = "Axnoded probe attempt duration."
	descVolumeOperationTotal                   = "Axnoded node volume operation results."
	descVolumeReconcileCurrent                 = "Axnoded last node volume reconcile counts."
)

const (
	descReadinessProbeStageDuration = "Axnoded readiness probe execution stage duration."
	descNetworkNeighborResetTotal   = "Axnoded bridge neighbor reset attempts."
)

type Point struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	Value       float64           `json:"value,omitempty"`
	Count       uint64            `json:"count,omitempty"`
	Sum         float64           `json:"sum,omitempty"`
	SampleStart uint64            `json:"sampleStart,omitempty"`
	Samples     []float64         `json:"samples,omitempty"`
}

type Snapshot struct {
	Version        string    `json:"version"`
	CollectedAt    time.Time `json:"collectedAt"`
	DroppedRecords uint64    `json:"droppedRecords,omitempty"`
	Points         []Point   `json:"points,omitempty"`
}

type pointKey struct {
	name string
	kind string
	key  string
}

type snapshotRecorder struct {
	mu         sync.Mutex
	counters   map[pointKey]*Point
	gauges     map[pointKey]*Point
	histograms map[pointKey]*Point
	dropped    uint64
}

var debugRecorder = &snapshotRecorder{
	counters:   map[pointKey]*Point{},
	gauges:     map[pointKey]*Point{},
	histograms: map[pointKey]*Point{},
}

type resourceAllocateStageObservation struct {
	resource string
	stage    string
	result   string
	seconds  float64
	barrier  chan struct{}
}

var resourceAllocateStageObservations = make(chan resourceAllocateStageObservation, 4096)

func init() {
	go func() {
		for observation := range resourceAllocateStageObservations {
			if observation.barrier != nil {
				close(observation.barrier)
				continue
			}
			recordResourceAllocateStage(observation.resource, observation.stage, observation.result, observation.seconds)
		}
	}()
}

func SnapshotCurrent() Snapshot {
	return debugRecorder.snapshot()
}

func ResetForTest() {
	flushResourceAllocateStagesForTest()
	debugRecorder.reset()
}

func CounterValueForTest(name string, attrs map[string]string) float64 {
	point, ok := debugRecorder.find(TypeCounter, name, attrs)
	if !ok {
		return 0
	}
	return point.Value
}

func GaugeValueForTest(name string, attrs map[string]string) float64 {
	point, ok := debugRecorder.find(TypeGauge, name, attrs)
	if !ok {
		return 0
	}
	return point.Value
}

func HistogramCountForTest(name string, attrs map[string]string) uint64 {
	point, ok := debugRecorder.find(TypeHistogram, name, attrs)
	if !ok {
		return 0
	}
	return point.Count
}

func RecordActionLatencyMs(action string, cost int64) {
	recordDuration(
		MetricSandboxActionDuration,
		descSandboxActionDuration,
		time.Duration(cost)*time.Millisecond,
		attribute.String(sdkobs.AttrOperation, action),
	)
}

func RecordExecutionLeaseVisibility(duration time.Duration, result string) {
	recordDuration(
		MetricExecutionLeaseVisibilityDuration,
		descExecutionLeaseVisibilityDuration,
		duration,
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordActionResult(action string, result string) {
	recordCounter(
		MetricSandboxActionTotal,
		descSandboxActionTotal,
		attribute.String(sdkobs.AttrOperation, action),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordResourceGauge(resourceType string, value float64) {
	recordGauge(
		MetricSandboxResourceCurrent,
		descSandboxResourceCurrent,
		value,
		attribute.String(sdkobs.AttrResource, resourceType),
	)
}

func RecordRuntimeCallResult(action string, result string, runtime string) {
	recordCounter(
		MetricRuntimeCallTotal,
		descRuntimeCallTotal,
		attribute.String(sdkobs.AttrOperation, action),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrRuntime, runtime),
	)
}

func RecordGcQueueLength(resourceType string, value float64) {
	recordGauge(
		MetricGCQueueCurrent,
		descGCQueueCurrent,
		value,
		attribute.String(sdkobs.AttrResource, resourceType),
	)
}

func RecordStartDuration(startClass, runtime, rootfsType, result string, seconds float64) {
	recordDurationSeconds(
		MetricStartupDuration,
		descStartupDuration,
		seconds,
		startAttrs(startClass, runtime, rootfsType, result)...,
	)
}

func RecordStartPhaseDuration(phase, startClass, runtime, rootfsType, result string, seconds float64) {
	attrs := append(
		[]attribute.KeyValue{attribute.String(sdkobs.AttrPhase, phase)},
		startAttrs(startClass, runtime, rootfsType, result)...,
	)
	recordDurationSeconds(MetricStartupPhaseDuration, descStartupPhaseDuration, seconds, attrs...)
}

func RecordStartStepDuration(phase, step, startClass, runtime, rootfsType, result string, seconds float64) {
	attrs := append(
		[]attribute.KeyValue{
			attribute.String(sdkobs.AttrPhase, phase),
			attribute.String(sdkobs.AttrStep, step),
		},
		startAttrs(startClass, runtime, rootfsType, result)...,
	)
	recordDurationSeconds(MetricStartupStepDuration, descStartupStepDuration, seconds, attrs...)
}

func RecordStartResult(startClass, runtime, rootfsType, result string) {
	recordCounter(MetricStartupTotal, descStartupTotal, startAttrs(startClass, runtime, rootfsType, result)...)
}

func RecordLifecycleStageDuration(operation, stage, runtime, result, errorClass string, seconds float64) {
	recordDurationSeconds(
		MetricLifecycleStageDuration,
		descLifecycleStageDuration,
		seconds,
		attribute.String(sdkobs.AttrOperation, operation),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrRuntime, runtime),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}

func RecordAllocationDeleteStage(stage, runtime, result string, seconds float64) {
	recordDurationSeconds(
		MetricAllocationDeleteStageDuration,
		descAllocationDeleteStageDuration,
		seconds,
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrRuntime, runtime),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordHTTPProxyStageDuration(stage, result, errorClass string, seconds float64) {
	recordDurationSeconds(
		MetricHTTPProxyStageDuration,
		descHTTPProxyStageDuration,
		seconds,
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}

func RecordRetainedRuntimeGauge(rootfsType string, value float64) {
	recordGauge(MetricRetainedRuntimeCurrent, descRetainedRuntimeCurrent, value, attribute.String(sdkobs.AttrRootFSType, rootfsType))
}

func RecordRetainedRootfsGauge(rootfsType string, value float64) {
	recordGauge(MetricRetainedRootfsCurrent, descRetainedRootfsCurrent, value, attribute.String(sdkobs.AttrRootFSType, rootfsType))
}

func RecordRetentionReuse(kind, rootfsType string) {
	recordCounter(
		MetricRetentionReuseTotal,
		descRetentionReuseTotal,
		attribute.String(sdkobs.AttrKind, kind),
		attribute.String(sdkobs.AttrRootFSType, rootfsType),
	)
}

func RecordRetentionEviction(kind, rootfsType, reason string) {
	recordCounter(
		MetricRetentionEvictionTotal,
		descRetentionEvictionTotal,
		attribute.String(sdkobs.AttrKind, kind),
		attribute.String(sdkobs.AttrRootFSType, rootfsType),
		attribute.String(sdkobs.AttrReason, reason),
	)
}

func RecordResourcePoolState(resource string, idle, using, target int) {
	attr := attribute.String(sdkobs.AttrResource, resource)
	recordGauge(MetricResourcePoolIdleCurrent, descResourcePoolIdleCurrent, float64(idle), attr)
	recordGauge(MetricResourcePoolUsingCurrent, descResourcePoolUsingCurrent, float64(using), attr)
	recordGauge(MetricResourcePoolTargetCurrent, descResourcePoolTargetCurrent, float64(target), attr)
}

func RecordResourcePoolAllocate(resource, result string) {
	recordCounter(
		MetricResourcePoolAllocateTotal,
		descResourcePoolAllocateTotal,
		attribute.String(sdkobs.AttrResource, resource),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordResourceAllocateStage(resource, stage, result string, seconds float64) {
	select {
	case resourceAllocateStageObservations <- resourceAllocateStageObservation{
		resource: resource,
		stage:    stage,
		result:   result,
		seconds:  seconds,
	}:
	default:
		sdkobs.Int64Counter(MetricResourceAllocateObservationDropped, descResourceAllocateObservationDropped).Add(context.Background(), 1)
	}
}

func recordResourceAllocateStage(resource, stage, result string, seconds float64) {
	recordDurationSeconds(
		MetricResourceAllocateStageDuration,
		descResourceAllocateStageDuration,
		seconds,
		attribute.String(sdkobs.AttrResource, resource),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func flushResourceAllocateStagesForTest() {
	barrier := make(chan struct{})
	resourceAllocateStageObservations <- resourceAllocateStageObservation{barrier: barrier}
	<-barrier
}

func RecordResourcePoolRefill(resource, trigger, result string, seconds float64) {
	attrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrResource, resource),
		attribute.String(sdkobs.AttrTrigger, trigger),
		attribute.String(sdkobs.AttrResult, result),
	}
	recordCounter(MetricResourcePoolRefillTotal, descResourcePoolRefillTotal, attrs...)
	recordDurationSeconds(MetricResourcePoolRefillDuration, descResourcePoolRefillDuration, seconds, attrs...)
}

func RecordBundleTemplate(runtime, rootfsType, result string) {
	recordCounter(
		MetricBundleTemplateTotal,
		descBundleTemplateTotal,
		runtimeRootfsResultAttrs(runtime, rootfsType, result)...,
	)
}

func RecordBundleMaterializeDuration(runtime, rootfsType, result string, seconds float64) {
	recordDurationSeconds(
		MetricBundleMaterializeDuration,
		descBundleMaterializeDuration,
		seconds,
		runtimeRootfsResultAttrs(runtime, rootfsType, result)...,
	)
}

func RecordExecutionEnvelope(runtime, rootfsType, result string) {
	recordCounter(
		MetricExecutionEnvelopeTotal,
		descExecutionEnvelopeTotal,
		runtimeRootfsResultAttrs(runtime, rootfsType, result)...,
	)
}

func RecordExecutionEnvelopePrepareDuration(runtime, rootfsType, result string, seconds float64) {
	recordDurationSeconds(
		MetricExecutionEnvelopePrepare,
		descExecutionEnvelopePrepare,
		seconds,
		runtimeRootfsResultAttrs(runtime, rootfsType, result)...,
	)
}

func RecordExecutionEnvelopeActivateDuration(runtime, rootfsType, result string, seconds float64) {
	recordDurationSeconds(
		MetricExecutionEnvelopeActivate,
		descExecutionEnvelopeActivate,
		seconds,
		runtimeRootfsResultAttrs(runtime, rootfsType, result)...,
	)
}

func RecordExecutionEnvelopeGauge(runtime, rootfsType, state string, value float64) {
	recordGauge(
		MetricExecutionEnvelopeCurrent,
		descExecutionEnvelopeCurrent,
		value,
		attribute.String(sdkobs.AttrRuntime, runtime),
		attribute.String(sdkobs.AttrRootFSType, rootfsType),
		attribute.String(sdkobs.AttrState, state),
	)
}

func RecordRuntimeWaitGrace(runtime, result string) {
	recordCounter(
		MetricRuntimeWaitGraceTotal,
		descRuntimeWaitGraceTotal,
		attribute.String(sdkobs.AttrRuntime, runtime),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordControlPlaneRPC(rpc, result string) {
	recordCounter(
		MetricControlPlaneRPCTotal,
		descControlPlaneRPCTotal,
		attribute.String(sdkobs.AttrOperation, rpc),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordControlPlaneRPCDuration(rpc, result string, seconds float64) {
	recordDurationSeconds(
		MetricControlPlaneRPCDuration,
		descControlPlaneRPCDuration,
		seconds,
		attribute.String(sdkobs.AttrOperation, rpc),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordAllocationStatusQueueEvent(result string) {
	recordCounter(
		MetricAllocationStatusQueueTotal,
		descAllocationStatusQueueTotal,
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordAllocationStatusQueueCurrent(value int) {
	recordGauge(MetricAllocationStatusQueueCurrent, descAllocationStatusQueueCurrent, float64(value))
}

func RecordAllocationStatusQueueWait(result string, seconds float64) {
	recordDurationSeconds(
		MetricAllocationStatusQueueWait,
		descAllocationStatusQueueWait,
		seconds,
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordAllocationStatusBatch(result string, observations int) {
	recordCounter(
		MetricAllocationStatusBatchTotal,
		descAllocationStatusBatchTotal,
		attribute.String(sdkobs.AttrResult, result),
	)
	if observations > 0 {
		recordCounterValue(
			MetricAllocationStatusBatchObservationsTotal,
			descAllocationStatusBatchObservationsTotal,
			int64(observations),
			attribute.String(sdkobs.AttrResult, result),
		)
	}
}

func RecordAllocationStatusReporterHealth(oldestPendingAgeSeconds float64, consecutiveFailures int, retryDelaySeconds float64) {
	recordGauge(
		MetricAllocationStatusOldestPendingAge,
		descAllocationStatusOldestPendingAge,
		max(0, oldestPendingAgeSeconds),
	)
	recordGauge(
		MetricAllocationStatusConsecutiveFailures,
		descAllocationStatusConsecutiveFailures,
		float64(max(0, consecutiveFailures)),
	)
	recordGauge(
		MetricAllocationStatusRetryDelay,
		descAllocationStatusRetryDelay,
		max(0, retryDelaySeconds),
	)
}

func RecordReadinessWaitDuration(probeType, result string, seconds float64) {
	recordDurationSeconds(
		MetricReadinessWaitDuration,
		descReadinessWaitDuration,
		seconds,
		attribute.String(sdkobs.AttrProbeType, probeType),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordProbeAttemptDuration(probeKind, probeType, result string, seconds float64) {
	recordDurationSeconds(
		MetricProbeAttemptDuration,
		descProbeAttemptDuration,
		seconds,
		attribute.String(sdkobs.AttrProbeKind, probeKind),
		attribute.String(sdkobs.AttrProbeType, probeType),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordReadinessProbeStageDuration(probeType, stage, result, errorClass string, seconds float64) {
	recordDurationSeconds(
		MetricReadinessProbeStageDuration,
		descReadinessProbeStageDuration,
		seconds,
		attribute.String(sdkobs.AttrProbeType, probeType),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}

func RecordNetworkNeighborReset(trigger, result string) {
	recordCounter(
		MetricNetworkNeighborResetTotal,
		descNetworkNeighborResetTotal,
		attribute.String(sdkobs.AttrTrigger, trigger),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordVolumeOperation(operation, result string) {
	recordCounter(
		MetricVolumeOperationTotal,
		descVolumeOperationTotal,
		attribute.String(sdkobs.AttrOperation, operation),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func RecordVolumeReconcile(kind string, value float64) {
	recordGauge(MetricVolumeReconcileCurrent, descVolumeReconcileCurrent, value, attribute.String(sdkobs.AttrState, kind))
}

func startAttrs(startClass, runtime, rootfsType, result string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(sdkobs.AttrStartClass, startClass),
		attribute.String(sdkobs.AttrRuntime, runtime),
		attribute.String(sdkobs.AttrRootFSType, rootfsType),
		attribute.String(sdkobs.AttrResult, result),
	}
}

func runtimeRootfsResultAttrs(runtime, rootfsType, result string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(sdkobs.AttrRuntime, runtime),
		attribute.String(sdkobs.AttrRootFSType, rootfsType),
		attribute.String(sdkobs.AttrResult, result),
	}
}

func recordCounter(name, description string, attrs ...attribute.KeyValue) {
	recordCounterValue(name, description, 1, attrs...)
}

func recordCounterValue(name, description string, value int64, attrs ...attribute.KeyValue) {
	if value <= 0 {
		return
	}
	sdkobs.Int64Counter(name, description).Add(context.Background(), value, attrs...)
	debugRecorder.addCounter(name, attrs, float64(value))
}

func recordGauge(name, description string, value float64, attrs ...attribute.KeyValue) {
	sdkobs.Float64Gauge(name, description).Record(context.Background(), value, attrs...)
	debugRecorder.setGauge(name, attrs, value)
}

func recordDurationSeconds(name, description string, seconds float64, attrs ...attribute.KeyValue) {
	if seconds < 0 {
		seconds = 0
	}
	recordDuration(name, description, time.Duration(seconds*float64(time.Second)), attrs...)
}

func recordDuration(name, description string, duration time.Duration, attrs ...attribute.KeyValue) {
	if duration < 0 {
		duration = 0
	}
	sdkobs.DurationHistogram(name, description).RecordDuration(context.Background(), duration, attrs...)
	debugRecorder.addHistogramSample(name, attrs, duration.Seconds())
}

func (r *snapshotRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters = map[pointKey]*Point{}
	r.gauges = map[pointKey]*Point{}
	r.histograms = map[pointKey]*Point{}
	r.dropped = 0
}

func (r *snapshotRecorder) addCounter(name string, attrs []attribute.KeyValue, delta float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, attrMap := makePointKey(name, TypeCounter, attrs)
	point := r.counters[key]
	if point == nil {
		if !r.reserveSeries() {
			return
		}
		point = &Point{Name: name, Type: TypeCounter, Attributes: attrMap}
		r.counters[key] = point
	}
	point.Value += delta
}

func (r *snapshotRecorder) setGauge(name string, attrs []attribute.KeyValue, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, attrMap := makePointKey(name, TypeGauge, attrs)
	point := r.gauges[key]
	if point == nil {
		if !r.reserveSeries() {
			return
		}
		point = &Point{Name: name, Type: TypeGauge, Attributes: attrMap}
		r.gauges[key] = point
	}
	point.Value = value
}

func (r *snapshotRecorder) addHistogramSample(name string, attrs []attribute.KeyValue, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, attrMap := makePointKey(name, TypeHistogram, attrs)
	point := r.histograms[key]
	if point == nil {
		if !r.reserveSeries() {
			return
		}
		point = &Point{Name: name, Type: TypeHistogram, Attributes: attrMap}
		r.histograms[key] = point
	}
	point.Count++
	point.Sum += value
	if len(point.Samples) < maxDebugHistogramSamples {
		point.Samples = append(point.Samples, value)
		return
	}
	point.Samples[int((point.Count-1)%uint64(maxDebugHistogramSamples))] = value
	point.SampleStart = point.Count - uint64(len(point.Samples))
}

func (r *snapshotRecorder) snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	points := make([]Point, 0, len(r.counters)+len(r.gauges)+len(r.histograms))
	for _, point := range r.counters {
		points = append(points, clonePoint(point))
	}
	for _, point := range r.gauges {
		points = append(points, clonePoint(point))
	}
	for _, point := range r.histograms {
		points = append(points, clonePoint(point))
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Name != points[j].Name {
			return points[i].Name < points[j].Name
		}
		if points[i].Type != points[j].Type {
			return points[i].Type < points[j].Type
		}
		return attrsKeyFromMap(points[i].Attributes) < attrsKeyFromMap(points[j].Attributes)
	})
	return Snapshot{
		Version:        SnapshotVersion,
		CollectedAt:    time.Now().UTC(),
		DroppedRecords: r.dropped,
		Points:         points,
	}
}

func (r *snapshotRecorder) reserveSeries() bool {
	if len(r.counters)+len(r.gauges)+len(r.histograms) < maxDebugSeries {
		return true
	}
	r.dropped++
	return false
}

func (r *snapshotRecorder) find(kind, name string, attrs map[string]string) (Point, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := pointKey{name: name, kind: kind, key: attrsKeyFromMap(attrs)}
	var source map[pointKey]*Point
	switch kind {
	case TypeCounter:
		source = r.counters
	case TypeGauge:
		source = r.gauges
	case TypeHistogram:
		source = r.histograms
	default:
		return Point{}, false
	}
	point := source[key]
	if point == nil {
		return Point{}, false
	}
	return clonePoint(point), true
}

func clonePoint(point *Point) Point {
	cloned := Point{
		Name:        point.Name,
		Type:        point.Type,
		Value:       point.Value,
		Count:       point.Count,
		Sum:         point.Sum,
		SampleStart: point.SampleStart,
	}
	if len(point.Attributes) > 0 {
		cloned.Attributes = make(map[string]string, len(point.Attributes))
		for key, value := range point.Attributes {
			cloned.Attributes[key] = value
		}
	}
	if len(point.Samples) > 0 {
		if point.SampleStart == 0 {
			cloned.Samples = append([]float64(nil), point.Samples...)
		} else {
			next := int(point.Count % uint64(len(point.Samples)))
			cloned.Samples = make([]float64, 0, len(point.Samples))
			cloned.Samples = append(cloned.Samples, point.Samples[next:]...)
			cloned.Samples = append(cloned.Samples, point.Samples[:next]...)
		}
	}
	return cloned
}

func makePointKey(name, kind string, attrs []attribute.KeyValue) (pointKey, map[string]string) {
	attrMap := attrsMap(attrs)
	return pointKey{name: name, kind: kind, key: attrsKeyFromMap(attrMap)}, attrMap
}

func attrsMap(attrs []attribute.KeyValue) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for _, attr := range sdkobs.SafeAttrs(attrs...) {
		out[string(attr.Key)] = sdkobs.SanitizeValue(attr.Value.Emit())
	}
	return out
}

func attrsKeyFromMap(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(strconv.Quote(key))
		builder.WriteByte('=')
		builder.WriteString(strconv.Quote(attrs[key]))
		builder.WriteByte('\n')
	}
	return builder.String()
}
