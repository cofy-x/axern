package adminkernel

import (
	"fmt"
	"strings"
	"time"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
)

const (
	DefaultStorageBindingListLimit = 50
	MaxStorageBindingListLimit     = 200
)

type StorageBinding struct {
	BindingID    string
	ClaimID      string
	Namespace    string
	ClaimName    string
	WorkloadID   string
	WorkloadType string
	AllocationID string
	NodeID       string
	Status       storagev1.VolumeStatus
	Message      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	PublishedAt  time.Time
	ReleasedAt   time.Time
}

type StorageBindingFilter struct {
	Statuses     []storagev1.VolumeStatus
	Namespace    string
	ClaimName    string
	WorkloadID   string
	AllocationID string
	NodeID       string
	Limit        int
}

type RetryStorageBindingRequest struct {
	BindingID      string
	OperatorReason string
}

type StorageReclaim struct {
	ClaimID     string
	Namespace   string
	ClaimName   string
	ServiceID   string
	NodeID      string
	Attempt     int64
	NextRetryAt time.Time
	LastError   string
	UpdatedAt   time.Time
}

type StorageReclaimFilter struct {
	Namespace string
	ServiceID string
	NodeID    string
	Limit     int
}

func NormalizeStorageReclaimFilter(in StorageReclaimFilter) StorageReclaimFilter {
	out := StorageReclaimFilter{Namespace: strings.TrimSpace(in.Namespace), ServiceID: strings.TrimSpace(in.ServiceID), NodeID: strings.TrimSpace(in.NodeID), Limit: in.Limit}
	if out.Limit <= 0 {
		out.Limit = DefaultStorageBindingListLimit
	}
	if out.Limit > MaxStorageBindingListLimit {
		out.Limit = MaxStorageBindingListLimit
	}
	return out
}

func NormalizeStorageBindingFilter(in StorageBindingFilter) StorageBindingFilter {
	out := StorageBindingFilter{
		Statuses:     append([]storagev1.VolumeStatus(nil), in.Statuses...),
		Namespace:    strings.TrimSpace(in.Namespace),
		ClaimName:    strings.TrimSpace(in.ClaimName),
		WorkloadID:   strings.TrimSpace(in.WorkloadID),
		AllocationID: strings.TrimSpace(in.AllocationID),
		NodeID:       strings.TrimSpace(in.NodeID),
		Limit:        in.Limit,
	}
	if out.Limit <= 0 {
		out.Limit = DefaultStorageBindingListLimit
	}
	if out.Limit > MaxStorageBindingListLimit {
		out.Limit = MaxStorageBindingListLimit
	}
	return out
}

func ValidateStorageBindingFilter(filter StorageBindingFilter) error {
	seen := map[storagev1.VolumeStatus]struct{}{}
	for _, status := range filter.Statuses {
		if status == storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED {
			return fmt.Errorf("storage binding status filter is invalid")
		}
		if _, ok := seen[status]; ok {
			return fmt.Errorf("storage binding status filter %s is duplicated", status)
		}
		seen[status] = struct{}{}
	}
	if filter.Limit <= 0 {
		return fmt.Errorf("storage binding list limit must be positive")
	}
	if filter.Limit > MaxStorageBindingListLimit {
		return fmt.Errorf("storage binding list limit must be at most %d", MaxStorageBindingListLimit)
	}
	return nil
}

func NormalizeRetryStorageBindingRequest(in RetryStorageBindingRequest) RetryStorageBindingRequest {
	return RetryStorageBindingRequest{
		BindingID:      strings.TrimSpace(in.BindingID),
		OperatorReason: strings.TrimSpace(in.OperatorReason),
	}
}

func ValidateRetryStorageBindingRequest(req RetryStorageBindingRequest) error {
	if req.BindingID == "" {
		return fmt.Errorf("storage binding id is required")
	}
	if req.OperatorReason == "" {
		return fmt.Errorf("operator reason is required")
	}
	return nil
}
