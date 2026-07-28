package admin

import (
	"context"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc"
)

type ReliabilityClient interface {
	CheckConsistency(context.Context, *adminv1.CheckConsistencyRequest, ...grpc.CallOption) (*adminv1.CheckConsistencyResponse, error)
	GetAdminReliabilityHealth(context.Context, *adminv1.GetAdminReliabilityHealthRequest, ...grpc.CallOption) (*adminv1.GetAdminReliabilityHealthResponse, error)
}

type ReliabilityControl struct {
	client ReliabilityClient
}

func NewReliability(client ReliabilityClient) ReliabilityControl {
	return ReliabilityControl{client: client}
}

func (c ReliabilityControl) CheckConsistency(ctx context.Context) (*adminv1.CheckConsistencyResponse, error) {
	return c.client.CheckConsistency(ctx, &adminv1.CheckConsistencyRequest{})
}

func (c ReliabilityControl) Health(ctx context.Context) (*adminv1.GetAdminReliabilityHealthResponse, error) {
	return c.client.GetAdminReliabilityHealth(ctx, &adminv1.GetAdminReliabilityHealthRequest{})
}
