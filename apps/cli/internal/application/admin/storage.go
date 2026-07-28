package admin

import (
	"context"
	"fmt"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"google.golang.org/grpc"
)

type StorageClient interface {
	ListStorageBindings(context.Context, *adminv1.ListStorageBindingsRequest, ...grpc.CallOption) (*adminv1.ListStorageBindingsResponse, error)
	RetryStorageBinding(context.Context, *adminv1.RetryStorageBindingRequest, ...grpc.CallOption) (*adminv1.RetryStorageBindingResponse, error)
	ListStorageReclaims(context.Context, *adminv1.ListStorageReclaimsRequest, ...grpc.CallOption) (*adminv1.ListStorageReclaimsResponse, error)
}

type StorageReclaimListOptions struct {
	Namespace string
	ServiceID string
	NodeID    string
	Limit     int
}

type StorageControl struct {
	client StorageClient
}

type StorageBindingListOptions struct {
	Statuses     []string
	Namespace    string
	ClaimName    string
	WorkloadID   string
	AllocationID string
	NodeID       string
	Limit        int
}

func NewStorage(client StorageClient) StorageControl {
	return StorageControl{client: client}
}

func (c StorageControl) ListBindings(ctx context.Context, options StorageBindingListOptions) (*adminv1.ListStorageBindingsResponse, error) {
	return c.client.ListStorageBindings(ctx, &adminv1.ListStorageBindingsRequest{
		Filter: &adminv1.StorageBindingFilter{
			Statuses:     parseVolumeStatuses(options.Statuses),
			Namespace:    strings.TrimSpace(options.Namespace),
			ClaimName:    strings.TrimSpace(options.ClaimName),
			WorkloadID:   strings.TrimSpace(options.WorkloadID),
			AllocationID: strings.TrimSpace(options.AllocationID),
			NodeID:       strings.TrimSpace(options.NodeID),
		},
		Limit: int32(options.Limit),
	})
}

func (c StorageControl) RetryBinding(ctx context.Context, bindingID string, operatorReason string) (*adminv1.RetryStorageBindingResponse, error) {
	return c.client.RetryStorageBinding(ctx, &adminv1.RetryStorageBindingRequest{
		BindingID:      strings.TrimSpace(bindingID),
		OperatorReason: strings.TrimSpace(operatorReason),
	})
}

func (c StorageControl) ListReclaims(ctx context.Context, options StorageReclaimListOptions) (*adminv1.ListStorageReclaimsResponse, error) {
	return c.client.ListStorageReclaims(ctx, &adminv1.ListStorageReclaimsRequest{
		Namespace: strings.TrimSpace(options.Namespace), ServiceID: strings.TrimSpace(options.ServiceID), NodeID: strings.TrimSpace(options.NodeID), Limit: int32(options.Limit),
	})
}

func parseVolumeStatuses(values []string) []storagev1.VolumeStatus {
	out := make([]storagev1.VolumeStatus, 0, len(values))
	for _, value := range values {
		out = append(out, ParseVolumeStatus(value))
	}
	return out
}

func ParseVolumeStatus(value string) storagev1.VolumeStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending":
		return storagev1.VolumeStatus_VOLUME_STATUS_PENDING
	case "bound":
		return storagev1.VolumeStatus_VOLUME_STATUS_BOUND
	case "published":
		return storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED
	case "releasing":
		return storagev1.VolumeStatus_VOLUME_STATUS_RELEASING
	case "failed":
		return storagev1.VolumeStatus_VOLUME_STATUS_FAILED
	case "deleted":
		return storagev1.VolumeStatus_VOLUME_STATUS_DELETED
	default:
		return storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED
	}
}

func ValidateVolumeStatus(value string) error {
	if ParseVolumeStatus(value) == storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED {
		return fmt.Errorf("--status must be pending, bound, published, releasing, failed, or deleted")
	}
	return nil
}
