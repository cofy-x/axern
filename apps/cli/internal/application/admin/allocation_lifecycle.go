package admin

import (
	"context"
	"fmt"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc"
)

type AllocationLifecycleClient interface {
	ListAllocationLifecycleRetries(context.Context, *adminv1.ListAllocationLifecycleRetriesRequest, ...grpc.CallOption) (*adminv1.ListAllocationLifecycleRetriesResponse, error)
	ForceAllocationLifecycleRetry(context.Context, *adminv1.ForceAllocationLifecycleRetryRequest, ...grpc.CallOption) (*adminv1.ForceAllocationLifecycleRetryResponse, error)
	FailAllocationLifecycleRetry(context.Context, *adminv1.FailAllocationLifecycleRetryRequest, ...grpc.CallOption) (*adminv1.FailAllocationLifecycleRetryResponse, error)
	ClearAllocationLifecycleRetry(context.Context, *adminv1.ClearAllocationLifecycleRetryRequest, ...grpc.CallOption) (*adminv1.ClearAllocationLifecycleRetryResponse, error)
}

type AllocationLifecycleControl struct {
	client AllocationLifecycleClient
}

type LifecycleRetryListOptions struct {
	OwnerType string
	Reason    string
	DueOnly   bool
	Limit     int
}

func NewAllocationLifecycle(client AllocationLifecycleClient) AllocationLifecycleControl {
	return AllocationLifecycleControl{client: client}
}

func (c AllocationLifecycleControl) ListRetries(ctx context.Context, options LifecycleRetryListOptions) (*adminv1.ListAllocationLifecycleRetriesResponse, error) {
	return c.client.ListAllocationLifecycleRetries(ctx, &adminv1.ListAllocationLifecycleRetriesRequest{
		Filter: &adminv1.AllocationLifecycleRetryFilter{
			OwnerType: parseOwnerType(options.OwnerType),
			Reason:    ParseRetryReason(options.Reason),
			DueOnly:   options.DueOnly,
		},
		Limit: int32(options.Limit),
	})
}

func (c AllocationLifecycleControl) ForceRetry(ctx context.Context, allocationID string, reason string, operatorReason string) (*adminv1.ForceAllocationLifecycleRetryResponse, error) {
	return c.client.ForceAllocationLifecycleRetry(ctx, &adminv1.ForceAllocationLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(allocationID),
		Reason:         ParseRetryReason(reason),
		OperatorReason: strings.TrimSpace(operatorReason),
	})
}

func (c AllocationLifecycleControl) FailCreateRetry(ctx context.Context, allocationID string, operatorReason string) (*adminv1.FailAllocationLifecycleRetryResponse, error) {
	return c.client.FailAllocationLifecycleRetry(ctx, &adminv1.FailAllocationLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(allocationID),
		Reason:         adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		OperatorReason: strings.TrimSpace(operatorReason),
	})
}

func (c AllocationLifecycleControl) ClearRetry(ctx context.Context, allocationID string, reason string, operatorReason string) (*adminv1.ClearAllocationLifecycleRetryResponse, error) {
	return c.client.ClearAllocationLifecycleRetry(ctx, &adminv1.ClearAllocationLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(allocationID),
		Reason:         ParseRetryReason(reason),
		OperatorReason: strings.TrimSpace(operatorReason),
	})
}

func ParseRetryReason(value string) adminv1.AllocationLifecycleRetryReason {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "create":
		return adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE
	case "delete":
		return adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_DELETE
	default:
		return adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_UNSPECIFIED
	}
}

func ValidateRetryReason(value string) error {
	if ParseRetryReason(value) == adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_UNSPECIFIED {
		return fmt.Errorf("--reason must be create or delete")
	}
	return nil
}

func ValidateOperatorReason(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("--operator-reason is required")
	}
	return nil
}

func parseOwnerType(value string) adminv1.AllocationLifecycleRetryOwnerType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "run":
		return adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_RUN
	case "service":
		return adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_SERVICE
	default:
		return adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_UNSPECIFIED
	}
}

func ValidateOwnerType(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if parseOwnerType(value) == adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_UNSPECIFIED {
		return fmt.Errorf("--owner must be run or service")
	}
	return nil
}
