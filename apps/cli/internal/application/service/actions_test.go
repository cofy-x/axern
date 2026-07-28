package service

import (
	"context"
	"testing"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc"
)

func TestDeleteServiceUsesResponseIdentity(t *testing.T) {
	result, err := DeleteService(context.Background(), fakeServiceActionClient{}, "requested")
	if err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	if result.ServiceID != "deleted" || result.Service.GetID() != "deleted" {
		t.Fatalf("DeleteService() result = %+v", result)
	}
}

type fakeServiceActionClient struct{}

func (fakeServiceActionClient) DeleteService(context.Context, *servicev1.DeleteServiceRequest, ...grpc.CallOption) (*servicev1.DeleteServiceResponse, error) {
	return &servicev1.DeleteServiceResponse{Service: &servicev1.Service{ID: "deleted"}}, nil
}
