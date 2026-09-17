package admin

import (
	"context"
	"fmt"
	"strings"

	privateadminv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/admin/v1"
	"google.golang.org/grpc"
)

type AllocationLifecycleClient interface {
	ListAllocationLifecycleRetries(context.Context, *privateadminv1.ListAllocationLifecycleRetriesRequest, ...grpc.CallOption) (*privateadminv1.ListAllocationLifecycleRetriesResponse, error)
	ForceAllocationLifecycleRetry(context.Context, *privateadminv1.ForceAllocationLifecycleRetryRequest, ...grpc.CallOption) (*privateadminv1.ForceAllocationLifecycleRetryResponse, error)
	FailAllocationLifecycleRetry(context.Context, *privateadminv1.FailAllocationLifecycleRetryRequest, ...grpc.CallOption) (*privateadminv1.FailAllocationLifecycleRetryResponse, error)
	ClearAllocationLifecycleRetry(context.Context, *privateadminv1.ClearAllocationLifecycleRetryRequest, ...grpc.CallOption) (*privateadminv1.ClearAllocationLifecycleRetryResponse, error)
}

type AllocationLifecycleControl struct {
	client AllocationLifecycleClient
}

type LifecycleRetryListOptions struct {
	DueOnly bool
	Limit   int
}

func NewAllocationLifecycle(client AllocationLifecycleClient) AllocationLifecycleControl {
	return AllocationLifecycleControl{client: client}
}

func (c AllocationLifecycleControl) ListRetries(ctx context.Context, options LifecycleRetryListOptions) (*privateadminv1.ListAllocationLifecycleRetriesResponse, error) {
	return c.client.ListAllocationLifecycleRetries(ctx, &privateadminv1.ListAllocationLifecycleRetriesRequest{
		Filter: &privateadminv1.AllocationLifecycleRetryFilter{
			DueOnly: options.DueOnly,
		},
		Limit: int32(options.Limit),
	})
}

func (c AllocationLifecycleControl) ForceRetry(ctx context.Context, allocationID string, operatorReason string) (*privateadminv1.ForceAllocationLifecycleRetryResponse, error) {
	return c.client.ForceAllocationLifecycleRetry(ctx, &privateadminv1.ForceAllocationLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(allocationID),
		OperatorReason: strings.TrimSpace(operatorReason),
	})
}

func (c AllocationLifecycleControl) FailCreateRetry(ctx context.Context, allocationID string, operatorReason string) (*privateadminv1.FailAllocationLifecycleRetryResponse, error) {
	return c.client.FailAllocationLifecycleRetry(ctx, &privateadminv1.FailAllocationLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(allocationID),
		OperatorReason: strings.TrimSpace(operatorReason),
	})
}

func (c AllocationLifecycleControl) ClearRetry(ctx context.Context, allocationID string, operatorReason string) (*privateadminv1.ClearAllocationLifecycleRetryResponse, error) {
	return c.client.ClearAllocationLifecycleRetry(ctx, &privateadminv1.ClearAllocationLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(allocationID),
		OperatorReason: strings.TrimSpace(operatorReason),
	})
}

func ValidateOperatorReason(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("--operator-reason is required")
	}
	return nil
}
