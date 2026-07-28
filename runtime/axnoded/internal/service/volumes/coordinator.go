package volumes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"github.com/sirupsen/logrus"
)

type Publisher interface {
	PublishAll(context.Context, string, string, []*privatestoragev1.ResolvedNodeVolume) ([]*privatestoragev1.PublishedNodeVolume, error)
	ListPublishedVolumes(context.Context, string) ([]*privatestoragev1.PublishedNodeVolume, error)
	UnpublishAllocation(context.Context, string) ([]*privatestoragev1.VolumeReleaseObservation, error)
	ReconcileActiveAllocations(context.Context, []string) (*runtimevolumev1.ReconcileVolumesResponse, error)
	DeleteVolume(context.Context, string, storagev1.VolumeBackend, string) error
}

func (c *Coordinator) Delete(ctx context.Context, claimID string, backend storagev1.VolumeBackend, backendHandle string) error {
	if c == nil || c.publisher == nil {
		return fmt.Errorf("volume manager client is not configured")
	}
	if err := c.publisher.DeleteVolume(ctx, claimID, backend, backendHandle); err != nil {
		c.recordOperation("delete", "error")
		return err
	}
	c.recordOperation("delete", "ok")
	return nil
}

type Options struct {
	Publisher           Publisher
	ActiveAllocationIDs func() []string
	RecordOperation     func(operation, result string)
	RecordReconcile     func(name string, value float64)
	Logger              logrus.FieldLogger
}

type Coordinator struct {
	publisher           Publisher
	activeAllocationIDs func() []string
	recordOperation     func(operation, result string)
	recordReconcile     func(name string, value float64)
	logger              logrus.FieldLogger
}

type PublishResult struct {
	Published     []*privatestoragev1.PublishedNodeVolume
	RuntimeMounts []*runtime.Mount
}

func NewCoordinator(options Options) *Coordinator {
	recordOperation := options.RecordOperation
	if recordOperation == nil {
		recordOperation = metrics.RecordVolumeOperation
	}
	recordReconcile := options.RecordReconcile
	if recordReconcile == nil {
		recordReconcile = metrics.RecordVolumeReconcile
	}
	logger := options.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Coordinator{
		publisher:           options.Publisher,
		activeAllocationIDs: options.ActiveAllocationIDs,
		recordOperation:     recordOperation,
		recordReconcile:     recordReconcile,
		logger:              logger,
	}
}

func (c *Coordinator) PublishForStart(ctx context.Context, request *runtime.StartRequest) (PublishResult, error) {
	if request == nil || len(request.GetNodeVolumes()) == 0 {
		return PublishResult{}, nil
	}
	if c == nil || c.publisher == nil {
		return PublishResult{}, fmt.Errorf("volume manager client is not configured")
	}
	runtimeClass := ""
	if request.GetRuntimeTemplate() != nil {
		runtimeClass = request.GetRuntimeTemplate().GetSandbox()
	}
	published, err := c.publisher.PublishAll(ctx, request.GetContainerID(), runtimeClass, request.GetNodeVolumes())
	if err != nil {
		c.recordOperation("publish", "error")
		c.logger.WithError(err).WithFields(logrus.Fields{
			"allocation_id": request.GetContainerID(),
			"volume_count":  len(request.GetNodeVolumes()),
		}).Warn("publish node volumes failed")
		return PublishResult{}, err
	}
	c.recordOperation("publish", "ok")
	c.logger.WithFields(logrus.Fields{
		"allocation_id": request.GetContainerID(),
		"volume_count":  len(published),
	}).Info("published node volumes")
	return PublishResult{
		Published:     published,
		RuntimeMounts: RuntimeMounts(published),
	}, nil
}

func (c *Coordinator) PublishedForAllocation(ctx context.Context, allocationID string) ([]*privatestoragev1.PublishedNodeVolume, error) {
	if c == nil || c.publisher == nil {
		return nil, fmt.Errorf("volume manager client is not configured")
	}
	return c.publisher.ListPublishedVolumes(ctx, allocationID)
}

func (c *Coordinator) Unpublish(ctx context.Context, allocationID string) ([]*privatestoragev1.VolumeReleaseObservation, error) {
	if c == nil || c.publisher == nil {
		return nil, nil
	}
	observations, err := c.publisher.UnpublishAllocation(ctx, allocationID)
	if err != nil {
		c.recordOperation("unpublish", "error")
		c.logger.WithError(err).WithField("allocation_id", allocationID).Warn("unpublish node volumes failed")
		return nil, err
	}
	c.recordOperation("unpublish", "ok")
	c.logger.WithFields(logrus.Fields{
		"allocation_id":     allocationID,
		"observation_count": len(observations),
	}).Info("unpublished node volumes")
	return observations, nil
}

func (c *Coordinator) Reconcile(ctx context.Context) error {
	if c == nil || c.publisher == nil {
		return nil
	}
	active := []string(nil)
	if c.activeAllocationIDs != nil {
		active = c.activeAllocationIDs()
	}
	active = NormalizeAllocationIDs(active)
	startedAt := time.Now()
	resp, err := c.publisher.ReconcileActiveAllocations(ctx, active)
	if err != nil {
		c.recordOperation("reconcile", "error")
		c.logger.WithError(err).WithField("active_allocations", len(active)).Warn("reconcile node volumes failed")
		return err
	}
	c.recordOperation("reconcile", "ok")
	c.recordReconcile("active_allocations", float64(len(active)))
	c.recordReconcile("reported_active_allocations", float64(resp.GetActiveAllocationCount()))
	c.recordReconcile("retained", float64(resp.GetRetainedCount()))
	c.recordReconcile("unpublished", float64(resp.GetUnpublishedCount()))
	c.recordReconcile("stale_allocations", float64(resp.GetStaleAllocationCount()))
	c.recordReconcile("invalid_volumes", float64(resp.GetInvalidVolumeCount()))
	c.logger.WithFields(logrus.Fields{
		"active_allocations":          len(active),
		"reported_active_allocations": resp.GetActiveAllocationCount(),
		"retained":                    resp.GetRetainedCount(),
		"unpublished":                 resp.GetUnpublishedCount(),
		"stale_allocations":           resp.GetStaleAllocationCount(),
		"invalid_volumes":             resp.GetInvalidVolumeCount(),
		"duration":                    time.Since(startedAt).String(),
	}).Info("reconciled node volumes")
	return nil
}

func RuntimeMounts(published []*privatestoragev1.PublishedNodeVolume) []*runtime.Mount {
	if len(published) == 0 {
		return nil
	}
	mounts := make([]*runtime.Mount, 0, len(published))
	for _, volume := range published {
		if volume == nil {
			continue
		}
		mounts = append(mounts, &runtime.Mount{
			Type:    "bind",
			Source:  volume.GetHostPath(),
			Target:  volume.GetTarget(),
			Options: append([]string(nil), volume.GetOptions()...),
		})
	}
	return mounts
}

func NormalizeAllocationIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
