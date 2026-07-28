package service

import (
	"context"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc"
)

type ServiceClient interface {
	CreateService(context.Context, *servicev1.CreateServiceRequest, ...grpc.CallOption) (*servicev1.CreateServiceResponse, error)
	GetService(context.Context, *servicev1.GetServiceRequest, ...grpc.CallOption) (*servicev1.GetServiceResponse, error)
	ListServices(context.Context, *servicev1.ListServicesRequest, ...grpc.CallOption) (*servicev1.ListServicesResponse, error)
	UpdateService(context.Context, *servicev1.UpdateServiceRequest, ...grpc.CallOption) (*servicev1.UpdateServiceResponse, error)
	DeleteService(context.Context, *servicev1.DeleteServiceRequest, ...grpc.CallOption) (*servicev1.DeleteServiceResponse, error)
	ListServiceReplicas(context.Context, *servicev1.ListServiceReplicasRequest, ...grpc.CallOption) (*servicev1.ListServiceReplicasResponse, error)
	ListServiceEvents(context.Context, *servicev1.ListServiceEventsRequest, ...grpc.CallOption) (*servicev1.ListServiceEventsResponse, error)
}

type Control struct {
	client       ServiceClient
	environments environmentResolver
}

func New(client ServiceClient) Control {
	return Control{client: client}
}
