package service

import (
	"context"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc"
)

type ActionClient interface {
	DeleteService(context.Context, *servicev1.DeleteServiceRequest, ...grpc.CallOption) (*servicev1.DeleteServiceResponse, error)
}

type DeleteResult struct {
	ServiceID string
	Service   *servicev1.Service
}

func (c Control) ListServiceIDs(ctx context.Context, req *servicev1.ListServicesRequest) ([]string, error) {
	resp, err := c.client.ListServices(ctx, req)
	if err != nil {
		return nil, err
	}
	return ServiceIDs(resp.GetServices()), nil
}

func (c Control) DeleteServiceIDs(ctx context.Context, serviceIDs []string) ([]string, error) {
	affectedIDs := make([]string, 0, len(serviceIDs))
	for _, id := range serviceIDs {
		result, err := c.DeleteService(ctx, id)
		if err != nil {
			return nil, err
		}
		affectedIDs = append(affectedIDs, result.ServiceID)
	}
	return affectedIDs, nil
}

func (c Control) DeleteService(ctx context.Context, serviceID string) (DeleteResult, error) {
	return DeleteService(ctx, c.client, serviceID)
}

func DeleteService(ctx context.Context, client ActionClient, serviceID string) (DeleteResult, error) {
	resp, err := client.DeleteService(ctx, &servicev1.DeleteServiceRequest{ServiceID: serviceID})
	if err != nil {
		return DeleteResult{}, err
	}
	deletedID := resp.GetService().GetID()
	if deletedID == "" {
		deletedID = serviceID
	}
	return DeleteResult{ServiceID: deletedID, Service: resp.GetService()}, nil
}

func ResponseServiceID(responseID, fallbackID string) string {
	if responseID != "" {
		return responseID
	}
	return fallbackID
}

func ServiceIDs(services []*servicev1.Service) []string {
	ids := make([]string, 0, len(services))
	for _, svc := range services {
		if svc == nil || svc.GetID() == "" {
			continue
		}
		ids = append(ids, svc.GetID())
	}
	return ids
}
