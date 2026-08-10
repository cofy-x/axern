package controlplane

import (
	"context"
	"strings"
	"sync"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	reporterRPCTimeout      = 3 * time.Second
	inventoryChangeDebounce = 25 * time.Millisecond
)

type AllocationStatusReport struct {
	AllocationID     string
	Attempt          int64
	Status           commonv1.AllocationStatus
	ExitCode         int32
	ExitCodeKnown    bool
	Ready            bool
	ReadinessMessage string
	Message          string
	ObservedAt       time.Time
}

type AllocationCapabilityConditionReport struct {
	AllocationID string
	Attempt      int64
	ConditionSet *capabilityv1.CapabilityConditionSet
}

type RuntimeNamesFunc func() []string
type SnapshotFunc func() (nodeinventory.NodeInventorySnapshot, bool)
type SummaryBuilder func(nodeinventory.NodeInventorySnapshot) *nodev1.NodeSummary

type Reporter struct {
	target               string
	nodeID               string
	nodeTarget           string
	nodeAuthToken        string
	interval             time.Duration
	runtimeNames         RuntimeNamesFunc
	snapshot             SnapshotFunc
	summaryBuilder       SummaryBuilder
	refreshInventory     func()
	control              NodeControlClientProvider
	statusBatcher        *allocationStatusBatcher
	statusBatcherOnce    sync.Once
	conditionBatcher     *allocationConditionBatcher
	conditionBatcherOnce sync.Once

	stopCh    chan struct{}
	changeCh  chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
	wg        sync.WaitGroup
}

func NewReporter(
	target string,
	nodeID string,
	nodeTarget string,
	nodeAuthToken string,
	tlsCACert string,
	tlsCert string,
	tlsKey string,
	interval time.Duration,
	runtimeNames RuntimeNamesFunc,
	snapshot SnapshotFunc,
	summaryBuilder SummaryBuilder,
) *Reporter {
	target = strings.TrimSpace(target)
	nodeID = strings.TrimSpace(nodeID)
	if target == "" || nodeID == "" || runtimeNames == nil || snapshot == nil || summaryBuilder == nil {
		return nil
	}
	control, err := newNodeControlClientProvider(target, tlsCACert, tlsCert, tlsKey)
	if err != nil {
		logrus.WithError(err).Warn("control-plane reporter disabled")
		return nil
	}
	reporter := &Reporter{
		target:         target,
		nodeID:         nodeID,
		nodeTarget:     strings.TrimSpace(nodeTarget),
		nodeAuthToken:  strings.TrimSpace(nodeAuthToken),
		interval:       interval,
		runtimeNames:   runtimeNames,
		snapshot:       snapshot,
		summaryBuilder: summaryBuilder,
		control:        control,
		stopCh:         make(chan struct{}),
		changeCh:       make(chan struct{}, 1),
	}
	reporter.statusBatcher = newAllocationStatusBatcher(reporter.sendAllocationStatusBatch)
	reporter.conditionBatcher = newAllocationConditionBatcher(reporter.sendAllocationConditionBatch)
	return reporter
}

func (r *Reporter) Start() {
	if r == nil {
		return
	}
	r.startOnce.Do(func() {
		if r.changeCh == nil {
			r.changeCh = make(chan struct{}, 1)
		}
		r.ensureStatusBatcher().Start()
		r.ensureConditionBatcher().Start()
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.register()
			r.report()

			ticker := time.NewTicker(r.interval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					r.report()
				case <-r.changeCh:
					if !r.coalesceInventoryChanges() {
						return
					}
					if r.refreshInventory != nil {
						r.refreshInventory()
					}
					r.report()
				case <-r.stopCh:
					return
				}
			}
		}()
	})
}

func (r *Reporter) SetInventoryRefresh(refresh func()) {
	if r == nil {
		return
	}
	r.refreshInventory = refresh
}

func (r *Reporter) NotifyInventoryChanged() {
	if r == nil {
		return
	}
	if r.changeCh == nil {
		return
	}
	select {
	case r.changeCh <- struct{}{}:
	default:
	}
}

func (r *Reporter) coalesceInventoryChanges() bool {
	timer := time.NewTimer(inventoryChangeDebounce)
	defer timer.Stop()
	for {
		select {
		case <-r.changeCh:
		case <-timer.C:
			return true
		case <-r.stopCh:
			return false
		}
	}
}

func (r *Reporter) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		close(r.stopCh)
		r.wg.Wait()
		r.ensureStatusBatcher().Stop()
		r.ensureConditionBatcher().Stop()
		if r.control != nil {
			if err := r.control.Close(); err != nil {
				logrus.WithError(err).Warn("close control-plane reporter client")
			}
		}
	})
}

func (r *Reporter) register() {
	ctx, op := sdkobs.StartOperation(context.Background(), sdkobs.OperationConfig{
		Name:        sandboxobs.SpanControlPlaneRegister,
		SpanAttrs:   []attribute.KeyValue{attribute.String(sdkobs.AttrNodeID, r.nodeID)},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "register")},
		Counter:     sandboxobs.MetricControlPlaneReportTotal,
		Duration:    sandboxobs.MetricControlPlaneReportDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()
	req := &nodev1.RegisterNodeRequest{
		NodeID:        r.nodeID,
		Runtimes:      r.runtimeNames(),
		NodeTarget:    r.nodeTarget,
		NodeAuthToken: r.nodeAuthToken,
	}
	started := time.Now()
	if err := r.withClient(ctx, func(ctx context.Context, client nodev1.NodeControlClient) error {
		_, err := client.RegisterNode(ctx, req)
		return err
	}); err != nil {
		op.SetErrorStatus("register node")
		opErr = err
		metrics.RecordControlPlaneRPC("register", "error")
		metrics.RecordControlPlaneRPCDuration("register", "error", time.Since(started).Seconds())
		logrus.WithError(err).Warn("control-plane register failed")
		return
	}
	metrics.RecordControlPlaneRPC("register", "ok")
	metrics.RecordControlPlaneRPCDuration("register", "ok", time.Since(started).Seconds())
}

func (r *Reporter) report() {
	ctx, op := sdkobs.StartOperation(context.Background(), sdkobs.OperationConfig{
		Name:        sandboxobs.SpanControlPlaneReportNode,
		SpanAttrs:   []attribute.KeyValue{attribute.String(sdkobs.AttrNodeID, r.nodeID)},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "report_node")},
		Counter:     sandboxobs.MetricControlPlaneReportTotal,
		Duration:    sandboxobs.MetricControlPlaneReportDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()
	snapshot, ready := r.snapshot()
	if !ready {
		op.SetResult(sdkobs.ResultSkipped)
		metrics.RecordControlPlaneRPC("report", "skipped")
		return
	}
	summary := r.summaryBuilder(snapshot)
	if summary != nil && summary.GetCollectedAt() == nil && !snapshot.Node.CollectedAt.IsZero() {
		summary.CollectedAt = timestamppb.New(snapshot.Node.CollectedAt)
	}
	req := &nodev1.ReportNodeRequest{
		NodeID:        r.nodeID,
		Runtimes:      r.runtimeNames(),
		Summary:       summary,
		NodeTarget:    r.nodeTarget,
		NodeAuthToken: r.nodeAuthToken,
	}
	started := time.Now()
	if err := r.withClient(ctx, func(ctx context.Context, client nodev1.NodeControlClient) error {
		_, err := client.ReportNode(ctx, req)
		return err
	}); err != nil {
		op.SetErrorStatus("report node")
		opErr = err
		metrics.RecordControlPlaneRPC("report", "error")
		metrics.RecordControlPlaneRPCDuration("report", "error", time.Since(started).Seconds())
		logrus.WithError(err).Warn("control-plane report failed")
		return
	}
	metrics.RecordControlPlaneRPC("report", "ok")
	metrics.RecordControlPlaneRPCDuration("report", "ok", time.Since(started).Seconds())
}

func (r *Reporter) ReportAllocationStatus(report AllocationStatusReport) {
	allocationID := strings.TrimSpace(report.AllocationID)
	if r == nil || allocationID == "" || report.Attempt <= 0 {
		return
	}
	ready := report.Ready
	readinessMessage := validProtocolString(strings.TrimSpace(report.ReadinessMessage))
	if report.Status != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
		ready = false
		readinessMessage = ""
	}
	r.ensureStatusBatcher().Enqueue(&nodev1.AllocationStatusObservation{
		AllocationID:     allocationID,
		Attempt:          report.Attempt,
		Status:           report.Status,
		ExitCode:         report.ExitCode,
		ExitCodeKnown:    report.ExitCodeKnown,
		Ready:            ready,
		ReadinessMessage: readinessMessage,
		Message:          validProtocolString(report.Message),
		ObservedAt:       timestamppb.New(report.ObservedAt.UTC()),
	})
}

func (r *Reporter) ReportAllocationCapabilityConditions(report AllocationCapabilityConditionReport) {
	allocationID := strings.TrimSpace(report.AllocationID)
	if r == nil || allocationID == "" || report.Attempt <= 0 || report.ConditionSet == nil || report.ConditionSet.GetRevision() <= 0 {
		return
	}
	r.ensureConditionBatcher().Enqueue(&nodev1.AllocationCapabilityConditionReport{
		AllocationID: allocationID, Attempt: report.Attempt,
		ConditionSet: proto.Clone(report.ConditionSet).(*capabilityv1.CapabilityConditionSet),
	})
}

func (r *Reporter) AllocationStatusHealth() AllocationStatusReporterHealth {
	if r == nil {
		return AllocationStatusReporterHealth{Status: "disabled"}
	}
	return r.ensureStatusBatcher().Health()
}

func validProtocolString(value string) string {
	return strings.ToValidUTF8(value, "\uFFFD")
}

func (r *Reporter) ensureStatusBatcher() *allocationStatusBatcher {
	r.statusBatcherOnce.Do(func() {
		if r.statusBatcher == nil {
			r.statusBatcher = newAllocationStatusBatcher(r.sendAllocationStatusBatch)
		}
	})
	return r.statusBatcher
}

func (r *Reporter) ensureConditionBatcher() *allocationConditionBatcher {
	r.conditionBatcherOnce.Do(func() {
		if r.conditionBatcher == nil {
			r.conditionBatcher = newAllocationConditionBatcher(r.sendAllocationConditionBatch)
		}
	})
	return r.conditionBatcher
}

func (r *Reporter) sendAllocationConditionBatch(ctx context.Context, reports []*nodev1.AllocationCapabilityConditionReport) error {
	request := &nodev1.BatchReportAllocationCapabilityConditionsRequest{
		NodeID: r.nodeID, NodeAuthToken: r.nodeAuthToken, Reports: reports,
	}
	err := r.withClient(ctx, func(ctx context.Context, client nodev1.NodeControlClient) error {
		_, err := client.BatchReportAllocationCapabilityConditions(ctx, request)
		return err
	})
	result := "ok"
	if err != nil {
		result = "error"
		logrus.WithError(err).Warn("control-plane capability condition batch failed")
	}
	metrics.RecordControlPlaneRPC("batch_report_allocation_capability_conditions", result)
	return err
}

func (r *Reporter) sendAllocationStatusBatch(ctx context.Context, observations []*nodev1.AllocationStatusObservation) error {
	if len(observations) == 0 {
		return nil
	}
	req := &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        r.nodeID,
		NodeAuthToken: r.nodeAuthToken,
		Observations:  observations,
	}
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name: sandboxobs.SpanControlPlaneReportAllocation,
		SpanAttrs: []attribute.KeyValue{
			attribute.String(sdkobs.AttrNodeID, r.nodeID),
			attribute.Int("axern.batch_size", len(observations)),
		},
		MetricAttrs: []attribute.KeyValue{
			attribute.String(sdkobs.AttrOperation, "batch_report_allocation_status"),
		},
		Counter:  sandboxobs.MetricControlPlaneReportTotal,
		Duration: sandboxobs.MetricControlPlaneReportDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()
	started := time.Now()
	if err := r.withClient(ctx, func(ctx context.Context, client nodev1.NodeControlClient) error {
		_, err := client.BatchReportAllocationStatus(ctx, req)
		return err
	}); err != nil {
		op.SetErrorStatus("batch report allocation status")
		opErr = err
		metrics.RecordControlPlaneRPC("batch_report_allocation_status", "error")
		metrics.RecordControlPlaneRPCDuration("batch_report_allocation_status", "error", time.Since(started).Seconds())
		logrus.WithError(err).Warn("control-plane allocation status batch failed")
		return err
	}
	metrics.RecordControlPlaneRPC("batch_report_allocation_status", "ok")
	metrics.RecordControlPlaneRPCDuration("batch_report_allocation_status", "ok", time.Since(started).Seconds())
	return nil
}

func (r *Reporter) withClient(parent context.Context, fn func(context.Context, nodev1.NodeControlClient) error) error {
	ctx, cancel := context.WithTimeout(parent, reporterRPCTimeout)
	defer cancel()
	client, err := r.control.Client(ctx)
	if err != nil {
		return err
	}
	return fn(ctx, client)
}
