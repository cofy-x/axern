package run

import (
	"context"
	"strings"
	"testing"
	"time"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc"
)

func TestCreateResolvesInlineEnvironmentSpec(t *testing.T) {
	runs := &fakeRunClient{}
	environments := &fakeEnvironmentClient{createResponse: &environmentv1.CreateEnvironmentResponse{
		Environment: &environmentv1.Environment{ID: "env-created"},
	}}
	control := NewWithEnvironment(runs, environments)

	_, err := control.Create(context.Background(), CreateParams{
		Namespace: "default",
		Spec:      &environmentv1.EnvironmentSpec{Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if environments.createCalls != 1 {
		t.Fatalf("CreateEnvironment calls = %d, want 1", environments.createCalls)
	}
	if runs.createRequest.GetEnvironmentID() != "env-created" {
		t.Fatalf("run environment id = %q, want env-created", runs.createRequest.GetEnvironmentID())
	}
}

func TestCreateUsesExplicitEnvironmentID(t *testing.T) {
	runs := &fakeRunClient{}
	environments := &fakeEnvironmentClient{}
	control := NewWithEnvironment(runs, environments)

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
	if runs.createRequest.GetEnvironmentID() != "env-existing" {
		t.Fatalf("run environment id = %q, want env-existing", runs.createRequest.GetEnvironmentID())
	}
}

func TestWaitHandlesEmptyRunResponse(t *testing.T) {
	control := New(&fakeRunClient{
		getResponses: []*runv1.GetRunResponse{{}},
	})

	_, err := control.Wait(context.Background(), "run-1", WaitTargetRunning, time.Millisecond, nil)
	if err == nil {
		t.Fatal("Wait returned nil error, want timeout")
	}
	if !strings.Contains(err.Error(), "timed out waiting for run run-1") {
		t.Fatalf("Wait error = %v, want timeout", err)
	}
}

func TestParseWaitTargetAcceptsCaseInsensitiveInput(t *testing.T) {
	target, err := ParseWaitTarget(" Running ", WaitTargetTerminal)
	if err != nil {
		t.Fatalf("ParseWaitTarget returned error: %v", err)
	}
	if target != WaitTargetRunning {
		t.Fatalf("target = %q, want %q", target, WaitTargetRunning)
	}
}

type fakeRunClient struct {
	createRequest *runv1.CreateRunRequest
	getCalls      int
	getResponses  []*runv1.GetRunResponse
}

func (f *fakeRunClient) CreateRun(_ context.Context, req *runv1.CreateRunRequest, _ ...grpc.CallOption) (*runv1.CreateRunResponse, error) {
	f.createRequest = req
	return &runv1.CreateRunResponse{Run: &runv1.Run{ID: "run-1"}}, nil
}

func (f *fakeRunClient) GetRun(context.Context, *runv1.GetRunRequest, ...grpc.CallOption) (*runv1.GetRunResponse, error) {
	if len(f.getResponses) > 0 {
		index := f.getCalls
		if index >= len(f.getResponses) {
			index = len(f.getResponses) - 1
		}
		f.getCalls++
		return f.getResponses[index], nil
	}
	return &runv1.GetRunResponse{}, nil
}

func (f *fakeRunClient) ListRuns(context.Context, *runv1.ListRunsRequest, ...grpc.CallOption) (*runv1.ListRunsResponse, error) {
	return &runv1.ListRunsResponse{}, nil
}

func (f *fakeRunClient) CancelRun(context.Context, *runv1.CancelRunRequest, ...grpc.CallOption) (*runv1.CancelRunResponse, error) {
	return &runv1.CancelRunResponse{}, nil
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
