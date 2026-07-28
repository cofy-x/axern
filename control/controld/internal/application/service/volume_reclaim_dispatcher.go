package appservice

import (
	"context"
	"fmt"
	"sort"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultVolumeReclaimWorkers        = 8
	defaultVolumeReclaimWorkersPerNode = 2
	volumeReclaimPollInterval          = time.Second
	volumeReclaimDeleteTimeout         = 20 * time.Second
	volumeReclaimReportTimeout         = 8 * time.Second
)

type volumeReclaimCompletion struct {
	nodeID string
}

func (c *controller) RunVolumeReclaimDispatcher(ctx context.Context, leaseOwner string, workers, workersPerNode int) {
	if c.storage == nil || c.lifecycle == nil {
		return
	}
	if workers <= 0 {
		workers = defaultVolumeReclaimWorkers
	}
	if workersPerNode <= 0 {
		workersPerNode = defaultVolumeReclaimWorkersPerNode
	}
	completed := make(chan volumeReclaimCompletion, workers)
	activeByNode := map[string]int{}
	active := 0
	ticker := time.NewTicker(volumeReclaimPollInterval)
	defer ticker.Stop()
	stopping := false
	for {
		for !stopping && active < workers {
			excluded := saturatedVolumeReclaimNodes(activeByNode, workersPerNode)
			claimStarted := time.Now()
			reclaim, err := c.storage.ClaimVolumeReclaims(ctx, leaseOwner, excluded)
			c.recordVolumeReclaimClaim(ctx, claimStarted, err)
			if err != nil {
				logrus.WithError(err).Warn("claim volume reclaim work")
				break
			}
			if reclaim == nil {
				break
			}
			active++
			activeByNode[reclaim.GetNodeID()]++
			c.recordVolumeReclaimDispatcherCurrent(ctx, active, len(activeByNode))
			go c.executeVolumeReclaim(reclaim, completed)
		}
		if stopping && active == 0 {
			return
		}
		select {
		case result := <-completed:
			active--
			activeByNode[result.nodeID]--
			if activeByNode[result.nodeID] == 0 {
				delete(activeByNode, result.nodeID)
			}
			c.recordVolumeReclaimDispatcherCurrent(context.Background(), active, len(activeByNode))
		case <-ticker.C:
		case <-ctx.Done():
			stopping = true
		}
	}
}

func (c *controller) executeVolumeReclaim(reclaim *privatestoragev1.VolumeReclaim, completed chan<- volumeReclaimCompletion) {
	defer func() { completed <- volumeReclaimCompletion{nodeID: reclaim.GetNodeID()} }()
	started := time.Now()
	target := ""
	if c.nodeTarget != nil {
		target, _ = c.nodeTarget(reclaim.GetNodeID())
	}
	var deleteErr error
	if target == "" {
		deleteErr = fmt.Errorf("node target for storage topology node %q is unavailable", reclaim.GetNodeID())
	} else {
		deleteCtx, cancel := context.WithTimeout(context.Background(), volumeReclaimDeleteTimeout)
		deleteErr = c.lifecycle.DeleteVolume(deleteCtx, target, reclaim)
		cancel()
	}
	reportCtx, cancel := context.WithTimeout(context.Background(), volumeReclaimReportTimeout)
	reportErr := c.storage.ReportVolumeReclaim(reportCtx, reclaim, deleteErr == nil, errorMessage(deleteErr))
	cancel()
	c.recordVolumeReclaimExecution(context.Background(), started, deleteErr, reportErr)
	if reportErr != nil {
		logrus.WithError(reportErr).WithField("claim_id", reclaim.GetClaimID()).Warn("report volume reclaim result")
		return
	}
	if reclaim.GetWorkloadID() == "" {
		return
	}
	eventType := servicev1.ServiceEventType_SERVICE_EVENT_TYPE_VOLUME_RECLAIMED
	message := fmt.Sprintf("volume claim %s reclaimed", reclaim.GetClaimID())
	if deleteErr != nil {
		eventType = servicev1.ServiceEventType_SERVICE_EVENT_TYPE_VOLUME_RECLAIM_RETRY
		message = fmt.Sprintf("volume claim %s reclaim will retry: %s", reclaim.GetClaimID(), deleteErr)
	}
	eventCtx, eventCancel := context.WithTimeout(context.Background(), volumeReclaimReportTimeout)
	if err := c.recordEvent(eventCtx, servicekernel.NewServiceEvent(reclaim.GetWorkloadID(), "", eventType,
		servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_UNSPECIFIED, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
		message, time.Now().UTC())); err != nil {
		logrus.WithError(err).WithField("claim_id", reclaim.GetClaimID()).Warn("record volume reclaim event")
	}
	eventCancel()
	if c.notifyReconcile != nil {
		c.notifyReconcile(reclaim.GetWorkloadID())
	}
}

func saturatedVolumeReclaimNodes(active map[string]int, limit int) []string {
	out := make([]string, 0, len(active))
	for nodeID, count := range active {
		if count >= limit {
			out = append(out, nodeID)
		}
	}
	sort.Strings(out)
	return out
}

func (c *controller) recordVolumeReclaimDispatcherCurrent(ctx context.Context, active, activeNodes int) {
	gauge := sdkobs.Float64Gauge(ctrlobs.MetricVolumeReclaimDispatcherCurrent.Name, ctrlobs.MetricVolumeReclaimDispatcherCurrent.Description)
	gauge.Record(ctx, float64(active), attribute.String(sdkobs.AttrState, "active"))
	gauge.Record(ctx, float64(activeNodes), attribute.String(sdkobs.AttrState, "active_nodes"))
}

func (c *controller) recordVolumeReclaimClaim(ctx context.Context, started time.Time, err error) {
	result := sdkobs.ResultOK
	if err != nil {
		result = sdkobs.ResultError
	}
	sdkobs.DurationHistogram(ctrlobs.MetricVolumeReclaimClaimDuration.Name, ctrlobs.MetricVolumeReclaimClaimDuration.Description).
		RecordDuration(ctx, time.Since(started), attribute.String(sdkobs.AttrResult, result))
}

func (c *controller) recordVolumeReclaimExecution(ctx context.Context, started time.Time, deleteErr, reportErr error) {
	result := "success"
	if reportErr != nil {
		result = "report_error"
	} else if deleteErr != nil {
		result = "retry"
	}
	sdkobs.Int64Counter(ctrlobs.MetricVolumeReclaimTotal.Name, ctrlobs.MetricVolumeReclaimTotal.Description).
		Add(ctx, 1, attribute.String(sdkobs.AttrResult, result))
	sdkobs.DurationHistogram(ctrlobs.MetricVolumeReclaimDuration.Name, ctrlobs.MetricVolumeReclaimDuration.Description).
		RecordDuration(ctx, time.Since(started), attribute.String(sdkobs.AttrResult, result))
}
