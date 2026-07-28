package environment

import (
	"context"
	"testing"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc"
)

func TestResolveIDUsesExplicitEnvironmentID(t *testing.T) {
	client := &fakeEnvironmentClient{}
	control := New(client)

	id, err := control.ResolveID(context.Background(), ResolveParams{EnvironmentID: " env-123 "})
	if err != nil {
		t.Fatalf("ResolveID returned error: %v", err)
	}
	if id != "env-123" {
		t.Fatalf("ResolveID() = %q, want env-123", id)
	}
	if client.createCalls != 0 {
		t.Fatalf("CreateEnvironment called %d times, want 0", client.createCalls)
	}
}

func TestResolveIDCreatesEnvironmentFromSpec(t *testing.T) {
	client := &fakeEnvironmentClient{createResponse: &environmentv1.CreateEnvironmentResponse{
		Environment: &environmentv1.Environment{ID: "env-created"},
	}}
	control := New(client)

	id, err := control.ResolveID(context.Background(), ResolveParams{
		Spec: &environmentv1.EnvironmentSpec{Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("ResolveID returned error: %v", err)
	}
	if id != "env-created" {
		t.Fatalf("ResolveID() = %q, want env-created", id)
	}
	if client.createCalls != 1 {
		t.Fatalf("CreateEnvironment called %d times, want 1", client.createCalls)
	}
}

func TestResolveIDRejectsAmbiguousSource(t *testing.T) {
	control := New(&fakeEnvironmentClient{})

	_, err := control.ResolveID(context.Background(), ResolveParams{
		EnvironmentID: "env-123",
		Spec:          &environmentv1.EnvironmentSpec{Namespace: "default"},
	})
	if err == nil {
		t.Fatal("ResolveID returned nil error, want ambiguity error")
	}
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
