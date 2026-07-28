package service

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc"
)

func TestCreateResolvesInlineEnvironmentSpec(t *testing.T) {
	services := &fakeServiceClient{}
	environments := &fakeEnvironmentClient{createResponse: &environmentv1.CreateEnvironmentResponse{
		Environment: &environmentv1.Environment{ID: "env-created"},
	}}
	control := NewWithEnvironment(services, environments)

	_, err := control.Create(context.Background(), CreateParams{
		Namespace: "default",
		Spec:      &environmentv1.EnvironmentSpec{Namespace: "default"},
		Replicas:  2,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if environments.createCalls != 1 {
		t.Fatalf("CreateEnvironment calls = %d, want 1", environments.createCalls)
	}
	if services.createRequest.GetEnvironmentID() != "env-created" {
		t.Fatalf("service environment id = %q, want env-created", services.createRequest.GetEnvironmentID())
	}
	if services.createRequest.GetReplicas() != 2 {
		t.Fatalf("service replicas = %d, want 2", services.createRequest.GetReplicas())
	}
}

func TestCreateUsesExplicitEnvironmentID(t *testing.T) {
	services := &fakeServiceClient{}
	environments := &fakeEnvironmentClient{}
	control := NewWithEnvironment(services, environments)

	_, err := control.Create(context.Background(), CreateParams{
		Namespace:     "default",
		EnvironmentID: "env-existing",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if environments.createCalls != 0 {
		t.Fatalf("CreateEnvironment calls = %d, want 0", environments.createCalls)
	}
	if services.createRequest.GetEnvironmentID() != "env-existing" {
		t.Fatalf("service environment id = %q, want env-existing", services.createRequest.GetEnvironmentID())
	}
}

func TestWaitReadyReturnsReadySnapshot(t *testing.T) {
	services := &fakeServiceClient{
		getResponses: []*servicev1.GetServiceResponse{{
			Service: &servicev1.Service{
				ID:            "svc-1",
				Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
				Replicas:      1,
				ReadyReplicas: 1,
			},
		}},
		replicaResponses: []*servicev1.ListServiceReplicasResponse{{
			Replicas: []*servicev1.ServiceReplica{{
				ID:     "alloc-1",
				Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
				Ready:  true,
			}},
		}},
	}
	control := New(services)
	updates := 0

	snapshot, err := control.WaitReady(context.Background(), "svc-1", time.Second, func(WaitSnapshot) {
		updates++
	})
	if err != nil {
		t.Fatalf("WaitReady returned error: %v", err)
	}
	if snapshot.Service.GetID() != "svc-1" {
		t.Fatalf("service id = %q, want svc-1", snapshot.Service.GetID())
	}
	if len(snapshot.Replicas) != 1 {
		t.Fatalf("replica count = %d, want 1", len(snapshot.Replicas))
	}
	if updates != 1 {
		t.Fatalf("updates = %d, want 1", updates)
	}
}

type fakeServiceClient struct {
	createRequest    *servicev1.CreateServiceRequest
	getCalls         int
	getResponses     []*servicev1.GetServiceResponse
	replicaCalls     int
	replicaResponses []*servicev1.ListServiceReplicasResponse
	eventCalls       int
	eventResponses   []*servicev1.ListServiceEventsResponse
}

func (f *fakeServiceClient) CreateService(_ context.Context, req *servicev1.CreateServiceRequest, _ ...grpc.CallOption) (*servicev1.CreateServiceResponse, error) {
	f.createRequest = req
	return &servicev1.CreateServiceResponse{Service: &servicev1.Service{ID: "svc-1"}}, nil
}

func (f *fakeServiceClient) GetService(context.Context, *servicev1.GetServiceRequest, ...grpc.CallOption) (*servicev1.GetServiceResponse, error) {
	if len(f.getResponses) > 0 {
		index := f.getCalls
		if index >= len(f.getResponses) {
			index = len(f.getResponses) - 1
		}
		f.getCalls++
		return f.getResponses[index], nil
	}
	return &servicev1.GetServiceResponse{}, nil
}

func (f *fakeServiceClient) ListServices(context.Context, *servicev1.ListServicesRequest, ...grpc.CallOption) (*servicev1.ListServicesResponse, error) {
	return &servicev1.ListServicesResponse{}, nil
}

func (f *fakeServiceClient) UpdateService(context.Context, *servicev1.UpdateServiceRequest, ...grpc.CallOption) (*servicev1.UpdateServiceResponse, error) {
	return &servicev1.UpdateServiceResponse{}, nil
}

func (f *fakeServiceClient) DeleteService(context.Context, *servicev1.DeleteServiceRequest, ...grpc.CallOption) (*servicev1.DeleteServiceResponse, error) {
	return &servicev1.DeleteServiceResponse{}, nil
}

func (f *fakeServiceClient) ListServiceReplicas(context.Context, *servicev1.ListServiceReplicasRequest, ...grpc.CallOption) (*servicev1.ListServiceReplicasResponse, error) {
	if len(f.replicaResponses) > 0 {
		index := f.replicaCalls
		if index >= len(f.replicaResponses) {
			index = len(f.replicaResponses) - 1
		}
		f.replicaCalls++
		return f.replicaResponses[index], nil
	}
	return &servicev1.ListServiceReplicasResponse{}, nil
}

func (f *fakeServiceClient) ListServiceEvents(context.Context, *servicev1.ListServiceEventsRequest, ...grpc.CallOption) (*servicev1.ListServiceEventsResponse, error) {
	if len(f.eventResponses) > 0 {
		index := f.eventCalls
		if index >= len(f.eventResponses) {
			index = len(f.eventResponses) - 1
		}
		f.eventCalls++
		return f.eventResponses[index], nil
	}
	return &servicev1.ListServiceEventsResponse{}, nil
}

type fakeEnvironmentClient struct {
	createCalls    int
	createResponse *environmentv1.CreateEnvironmentResponse
}

func (f *fakeEnvironmentClient) CreateEnvironment(context.Context, *environmentv1.CreateEnvironmentRequest, ...grpc.CallOption) (*environmentv1.CreateEnvironmentResponse, error) {
	f.createCalls++
	if f.createResponse != nil {
		return f.createResponse, nil
	}
	return &environmentv1.CreateEnvironmentResponse{}, nil
}

func (f *fakeEnvironmentClient) GetEnvironment(context.Context, *environmentv1.GetEnvironmentRequest, ...grpc.CallOption) (*environmentv1.GetEnvironmentResponse, error) {
	return &environmentv1.GetEnvironmentResponse{}, nil
}

func (f *fakeEnvironmentClient) ListEnvironments(context.Context, *environmentv1.ListEnvironmentsRequest, ...grpc.CallOption) (*environmentv1.ListEnvironmentsResponse, error) {
	return &environmentv1.ListEnvironmentsResponse{}, nil
}
