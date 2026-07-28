package storagecoord

import (
	"context"
	"fmt"
	"strings"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	"github.com/cofy-x/axern/lib/go/grpcclient"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"
)

const DefaultTimeout = 10 * time.Second

type Coordinator interface {
	ResolveRequirements(ctx context.Context, namespace, serviceID string, config *commonv1.ExecutionConfig) ([]*privatestoragev1.VolumeRequirement, error)
	ReserveBindings(ctx context.Context, req servicekernel.StorageReserveRequest) ([]*privatestoragev1.ResolvedNodeVolume, error)
	ReportBindingPublish(ctx context.Context, allocationID, nodeID string, volumes []*privatestoragev1.PublishedNodeVolume) error
	ReportBindingPublishFailed(ctx context.Context, allocationID, nodeID string, volumes []*privatestoragev1.ResolvedNodeVolume, message string) error
	ReportBindingRelease(ctx context.Context, allocationID, nodeID string, observations []*privatestoragev1.VolumeReleaseObservation) error
	DeleteWorkloadVolumeClaims(ctx context.Context, namespace, serviceID string) (*privatestoragev1.DeleteWorkloadVolumeClaimsResponse, error)
	ClaimVolumeReclaims(ctx context.Context, leaseOwner string, excludedNodeIDs []string) (*privatestoragev1.VolumeReclaim, error)
	ReportVolumeReclaim(ctx context.Context, reclaim *privatestoragev1.VolumeReclaim, succeeded bool, message string) error
	VolumeReclaimQueueHealth(context.Context) (*privatestoragev1.VolumeReclaimQueueHealth, error)
}

func (c *Client) VolumeReclaimQueueHealth(ctx context.Context) (*privatestoragev1.VolumeReclaimQueueHealth, error) {
	if c == nil || c.client == nil {
		return &privatestoragev1.VolumeReclaimQueueHealth{}, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.client.GetVolumeReclaimQueueHealth(callCtx, &privatestoragev1.VolumeReclaimQueueHealthRequest{})
	if err != nil {
		return nil, err
	}
	return response.GetHealth(), nil
}

func (c *Client) StorageBindingHealth(ctx context.Context, releasingStuckAfter time.Duration) (adminkernel.StorageBindingHealth, error) {
	if c == nil || c.client == nil {
		return adminkernel.StorageBindingHealth{}, fmt.Errorf("storage coordinator client is not configured")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req := &privatestoragev1.VolumeBindingHealthRequest{}
	if releasingStuckAfter > 0 {
		req.ReleasingStuckAfter = durationpb.New(releasingStuckAfter)
	}
	resp, err := c.client.GetVolumeBindingHealth(callCtx, req)
	if err != nil {
		return adminkernel.StorageBindingHealth{}, err
	}
	health := resp.GetHealth()
	return adminkernel.StorageBindingHealth{
		FailedBindings:         health.GetFailedBindings(),
		ReleasingBindings:      health.GetReleasingBindings(),
		StuckReleasingBindings: health.GetStuckReleasingBindings(),
		InconsistentClaims:     health.GetInconsistentClaims(),
		InvalidBindings:        health.GetInvalidBindings(),
		DeletingClaims:         health.GetDeletingClaims(),
		StuckDeletingClaims:    health.GetStuckDeletingClaims(),
	}, nil
}

func (c *Client) ListStorageBindings(ctx context.Context, filter adminkernel.StorageBindingFilter) ([]adminkernel.StorageBinding, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("storage coordinator client is not configured")
	}
	filter = adminkernel.NormalizeStorageBindingFilter(filter)
	if err := adminkernel.ValidateStorageBindingFilter(filter); err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.client.ListVolumeBindings(callCtx, &privatestoragev1.ListVolumeBindingsRequest{
		Filter: &privatestoragev1.VolumeBindingListFilter{
			Statuses:     append([]storagev1.VolumeStatus(nil), filter.Statuses...),
			Namespace:    filter.Namespace,
			ClaimName:    filter.ClaimName,
			WorkloadID:   filter.WorkloadID,
			AllocationID: filter.AllocationID,
			NodeID:       filter.NodeID,
		},
		Limit: int32(filter.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]adminkernel.StorageBinding, 0, len(resp.GetBindings()))
	for _, binding := range resp.GetBindings() {
		out = append(out, storageBindingFromPrivate(binding))
	}
	return out, nil
}

func (c *Client) ListStorageReclaims(ctx context.Context, filter adminkernel.StorageReclaimFilter) ([]adminkernel.StorageReclaim, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("storage coordinator client is not configured")
	}
	filter = adminkernel.NormalizeStorageReclaimFilter(filter)
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.client.ListVolumeReclaims(callCtx, &privatestoragev1.ListVolumeReclaimsRequest{
		Namespace: filter.Namespace, WorkloadID: filter.ServiceID, NodeID: filter.NodeID, Limit: int32(filter.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]adminkernel.StorageReclaim, 0, len(resp.GetReclaims()))
	for _, reclaim := range resp.GetReclaims() {
		item := adminkernel.StorageReclaim{
			ClaimID: reclaim.GetClaimID(), Namespace: reclaim.GetNamespace(), ClaimName: reclaim.GetClaimName(), ServiceID: reclaim.GetWorkloadID(),
			NodeID: reclaim.GetNodeID(), Attempt: reclaim.GetAttempt(), LastError: sanitizeStorageReclaimError(reclaim.GetLastError()),
		}
		if reclaim.GetNextRetryAt() != nil {
			item.NextRetryAt = reclaim.GetNextRetryAt().AsTime()
		}
		if reclaim.GetUpdatedAt() != nil {
			item.UpdatedAt = reclaim.GetUpdatedAt().AsTime()
		}
		out = append(out, item)
	}
	return out, nil
}

func sanitizeStorageReclaimError(message string) string {
	return sdkobs.SanitizeLogBody(message)
}

func (c *Client) RetryStorageBinding(ctx context.Context, req adminkernel.RetryStorageBindingRequest) (*adminkernel.StorageBinding, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("storage coordinator client is not configured")
	}
	req = adminkernel.NormalizeRetryStorageBindingRequest(req)
	if err := adminkernel.ValidateRetryStorageBindingRequest(req); err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.client.RetryFailedVolumeBinding(callCtx, &privatestoragev1.RetryFailedVolumeBindingRequest{
		BindingID:      req.BindingID,
		OperatorReason: req.OperatorReason,
	})
	if err != nil {
		return nil, err
	}
	out := storageBindingFromPrivate(resp.GetBinding())
	return &out, nil
}

type Client struct {
	conn    *grpc.ClientConn
	client  privatestoragev1.StorageCoordinatorClient
	timeout time.Duration
}

func storageBindingFromPrivate(binding *privatestoragev1.VolumeBinding) adminkernel.StorageBinding {
	if binding == nil {
		return adminkernel.StorageBinding{}
	}
	out := adminkernel.StorageBinding{
		BindingID:    binding.GetBindingID(),
		ClaimID:      binding.GetClaimID(),
		Namespace:    binding.GetNamespace(),
		ClaimName:    binding.GetClaimName(),
		WorkloadID:   binding.GetWorkloadID(),
		WorkloadType: binding.GetWorkloadType(),
		AllocationID: binding.GetAllocationID(),
		NodeID:       binding.GetNodeID(),
		Status:       binding.GetStatus(),
		Message:      binding.GetMessage(),
	}
	if binding.GetCreatedAt() != nil {
		out.CreatedAt = binding.GetCreatedAt().AsTime()
	}
	if binding.GetUpdatedAt() != nil {
		out.UpdatedAt = binding.GetUpdatedAt().AsTime()
	}
	if binding.GetPublishedAt() != nil {
		out.PublishedAt = binding.GetPublishedAt().AsTime()
	}
	if binding.GetReleasedAt() != nil {
		out.ReleasedAt = binding.GetReleasedAt().AsTime()
	}
	return out
}

func NewClient(target string, opts ...Option) (*Client, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("storaged target is required")
	}
	cfg := clientConfig{timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	dialCtx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	conn, err := grpcclient.NewReadyClient(
		dialCtx,
		target,
		grpc.WithNoProxy(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial storaged %s: %w", target, err)
	}
	return &Client{conn: conn, client: privatestoragev1.NewStorageCoordinatorClient(conn), timeout: cfg.timeout}, nil
}

type clientConfig struct {
	timeout time.Duration
}

type Option func(*clientConfig)

func WithTimeout(timeout time.Duration) Option {
	return func(cfg *clientConfig) {
		if timeout > 0 {
			cfg.timeout = timeout
		}
	}
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) ResolveRequirements(ctx context.Context, namespace, serviceID string, config *commonv1.ExecutionConfig) ([]*privatestoragev1.VolumeRequirement, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.client.ResolveVolumeRequirements(callCtx, &privatestoragev1.ResolveVolumeRequirementsRequest{
		Namespace:    namespace,
		WorkloadID:   serviceID,
		WorkloadType: "service",
		Mounts:       workloadMounts(config),
	})
	if err != nil {
		return nil, err
	}
	return resp.GetRequirements(), nil
}

func (c *Client) ReserveBindings(ctx context.Context, req servicekernel.StorageReserveRequest) ([]*privatestoragev1.ResolvedNodeVolume, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.client.ReserveVolumeBinding(callCtx, &privatestoragev1.ReserveVolumeBindingRequest{
		Namespace:    req.Namespace,
		WorkloadID:   req.ServiceID,
		WorkloadType: "service",
		AllocationID: req.AllocationID,
		NodeID:       req.NodeID,
		Mounts:       workloadMounts(req.Config),
	})
	if err != nil {
		return nil, err
	}
	return resp.GetVolumes(), nil
}

func (c *Client) ReportBindingRelease(ctx context.Context, allocationID, nodeID string, observations []*privatestoragev1.VolumeReleaseObservation) error {
	if c == nil || c.client == nil {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.client.ReportVolumeRelease(callCtx, &privatestoragev1.ReportVolumeReleaseRequest{
		AllocationID: strings.TrimSpace(allocationID),
		NodeID:       strings.TrimSpace(nodeID),
		Observations: observations,
	})
	return err
}

func (c *Client) DeleteWorkloadVolumeClaims(ctx context.Context, namespace, serviceID string) (*privatestoragev1.DeleteWorkloadVolumeClaimsResponse, error) {
	if c == nil || c.client == nil {
		return &privatestoragev1.DeleteWorkloadVolumeClaimsResponse{Complete: true}, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.client.DeleteWorkloadVolumeClaims(callCtx, &privatestoragev1.DeleteWorkloadVolumeClaimsRequest{Namespace: namespace, WorkloadID: serviceID})
}

func (c *Client) ClaimVolumeReclaims(ctx context.Context, leaseOwner string, excludedNodeIDs []string) (*privatestoragev1.VolumeReclaim, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.client.ClaimVolumeReclaims(callCtx, &privatestoragev1.ClaimVolumeReclaimsRequest{
		Limit: 1, LeaseOwner: strings.TrimSpace(leaseOwner), ExcludedNodeIds: append([]string(nil), excludedNodeIDs...),
	})
	if err != nil {
		return nil, err
	}
	if len(response.GetReclaims()) == 0 {
		return nil, nil
	}
	return response.GetReclaims()[0], nil
}

func (c *Client) ReportVolumeReclaim(ctx context.Context, reclaim *privatestoragev1.VolumeReclaim, succeeded bool, message string) error {
	if c == nil || c.client == nil || reclaim == nil {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.client.ReportVolumeReclaim(callCtx, &privatestoragev1.ReportVolumeReclaimRequest{
		ClaimID: reclaim.GetClaimID(), NodeID: reclaim.GetNodeID(), Succeeded: succeeded, Message: strings.TrimSpace(message),
		LeaseToken: reclaim.GetLeaseToken(), LeaseOwner: reclaim.GetLeaseOwner(), LeaseGeneration: reclaim.GetLeaseGeneration(),
	})
	return err
}

func (c *Client) ReportBindingPublish(ctx context.Context, allocationID, nodeID string, volumes []*privatestoragev1.PublishedNodeVolume) error {
	if c == nil || c.client == nil || len(volumes) == 0 {
		return nil
	}
	observations := make([]*privatestoragev1.VolumePublishObservation, 0, len(volumes))
	for _, volume := range volumes {
		if volume == nil {
			continue
		}
		observations = append(observations, &privatestoragev1.VolumePublishObservation{
			BindingID:       volume.GetBindingID(),
			Status:          storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
			PublishedVolume: volume,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.client.ReportVolumePublish(callCtx, &privatestoragev1.ReportVolumePublishRequest{
		AllocationID: strings.TrimSpace(allocationID),
		NodeID:       strings.TrimSpace(nodeID),
		Observations: observations,
	})
	return err
}

func (c *Client) ReportBindingPublishFailed(ctx context.Context, allocationID, nodeID string, volumes []*privatestoragev1.ResolvedNodeVolume, message string) error {
	if c == nil || c.client == nil || len(volumes) == 0 {
		return nil
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "volume publish failed: node volume publish returned no failure message"
	}
	observations := make([]*privatestoragev1.VolumePublishObservation, 0, len(volumes))
	for _, volume := range volumes {
		if volume == nil {
			continue
		}
		observations = append(observations, &privatestoragev1.VolumePublishObservation{
			BindingID: volume.GetBindingID(),
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
			Message:   message,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.client.ReportVolumePublish(callCtx, &privatestoragev1.ReportVolumePublishRequest{
		AllocationID: strings.TrimSpace(allocationID),
		NodeID:       strings.TrimSpace(nodeID),
		Observations: observations,
	})
	return err
}

func workloadMounts(config *commonv1.ExecutionConfig) []*privatestoragev1.WorkloadVolumeMount {
	if config == nil {
		return nil
	}
	out := make([]*privatestoragev1.WorkloadVolumeMount, 0, len(config.GetVolumeMounts()))
	for _, mount := range config.GetVolumeMounts() {
		if mount == nil {
			continue
		}
		out = append(out, &privatestoragev1.WorkloadVolumeMount{
			ClaimName:     mount.GetName(),
			Target:        mount.GetTarget(),
			Readonly:      mount.GetReadonly(),
			Options:       append([]string(nil), mount.GetOptions()...),
			ReclaimPolicy: mount.GetReclaimPolicy(),
		})
	}
	return out
}

func RequirementsConstrainNode(requirements []*privatestoragev1.VolumeRequirement, nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	for _, req := range requirements {
		requiredNode := strings.TrimSpace(req.GetTopology().GetNodeID())
		if requiredNode != "" && requiredNode != nodeID {
			return false
		}
	}
	return true
}

func LocalDefaultClassRequest() *storagev1.CreateVolumeClassRequest {
	return &storagev1.CreateVolumeClassRequest{
		Name:                 "local",
		Backend:              storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		AccessModes:          []storagev1.VolumeAccessMode{storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE},
		DefaultReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		ConsistencyProfile:   storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{
			SupportsRunc:  true,
			SupportsRunsc: true,
		},
	}
}
