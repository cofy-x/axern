package run

import (
	"context"
	"io"
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
	updates := 0
	control := New(&fakeRunClient{
		getResponses: []*runv1.GetRunResponse{{}},
	})

	_, err := control.Wait(context.Background(), "run-1", WaitTargetRunning, time.Millisecond, func(*runv1.Run) {
		updates++
	})
	if err == nil {
		t.Fatal("Wait returned nil error, want empty response error")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("Wait error = %v, want empty response", err)
	}
	if updates != 0 {
		t.Fatalf("onUpdate calls = %d, want 0 for an invalid response", updates)
	}
}

func TestWaitReturnsInitialTerminalSnapshotWithoutWatch(t *testing.T) {
	runs := &fakeRunClient{getResponses: []*runv1.GetRunResponse{{Run: &runv1.Run{
		ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED, Version: 3,
	}}}}
	control := New(runs)

	result, err := control.Wait(context.Background(), "run-1", WaitTargetTerminal, time.Second, nil)
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if result.GetStatus() != runv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("Wait status = %s, want succeeded", result.GetStatus())
	}
	if runs.watchCalls != 0 {
		t.Fatalf("WatchRun calls = %d, want 0 for an initial terminal snapshot", runs.watchCalls)
	}
}

func TestWaitUsesWatchAfterInitialSnapshot(t *testing.T) {
	runs := &fakeRunClient{
		getResponses:   []*runv1.GetRunResponse{{Run: &runv1.Run{ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_PLACED, Version: 1}}},
		watchResponses: []*runv1.WatchRunResponse{{Run: &runv1.Run{ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_RUNNING, Version: 2}}},
	}
	control := New(runs)

	result, err := control.Wait(context.Background(), "run-1", WaitTargetRunning, time.Second, nil)
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if result.GetStatus() != runv1.RunStatus_RUN_STATUS_RUNNING {
		t.Fatalf("Wait status = %s, want running", result.GetStatus())
	}
	if runs.getCalls != 1 || runs.watchCalls != 1 {
		t.Fatalf("calls = get %d watch %d, want 1 each", runs.getCalls, runs.watchCalls)
	}
	if runs.watchRequest.GetRunID() != "run-1" || runs.watchRequest.GetAfterVersion() != 1 {
		t.Fatalf("WatchRun request = %+v, want run-1 after version 1", runs.watchRequest)
	}
}

func TestWaitRootfsSnapshotUsesRunVersionAndReturnsDerivedEnvironment(t *testing.T) {
	runs := &fakeRunClient{
		getResponses: []*runv1.GetRunResponse{{Run: &runv1.Run{
			ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED, Version: 3,
			RootfsSnapshot: &runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING},
		}}},
		watchResponses: []*runv1.WatchRunResponse{{Run: &runv1.Run{
			ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED, Version: 4,
			RootfsSnapshot: &runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY, EnvironmentID: "env-snapshot"},
		}}},
	}
	control := New(runs)

	result, err := control.Wait(context.Background(), "run-1", WaitTargetRootfsSnapshot, time.Second, nil)
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if result.GetRootfsSnapshot().GetEnvironmentID() != "env-snapshot" {
		t.Fatalf("snapshot environment = %q, want env-snapshot", result.GetRootfsSnapshot().GetEnvironmentID())
	}
	if runs.watchRequest.GetAfterVersion() != 3 {
		t.Fatalf("WatchRun after version = %d, want 3", runs.watchRequest.GetAfterVersion())
	}
}

func TestWaitRootfsSnapshotReturnsSnapshotFailure(t *testing.T) {
	runs := &fakeRunClient{getResponses: []*runv1.GetRunResponse{{Run: &runv1.Run{
		ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED, Version: 4,
		RootfsSnapshot: &runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_FAILED, Message: "registry denied"},
	}}}}

	_, err := New(runs).Wait(context.Background(), "run-1", WaitTargetRootfsSnapshot, time.Second, nil)
	if err == nil || !strings.Contains(err.Error(), "registry denied") {
		t.Fatalf("Wait error = %v, want snapshot diagnostic", err)
	}
}

func TestWaitReportsTerminalWatchFailure(t *testing.T) {
	runs := &fakeRunClient{
		getResponses: []*runv1.GetRunResponse{{Run: &runv1.Run{
			ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_RUNNING, Version: 2,
		}}},
		watchResponses: []*runv1.WatchRunResponse{{Run: &runv1.Run{
			ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_FAILED, Version: 3, Message: "runtime exited",
		}}},
	}
	control := New(runs)

	result, err := control.Wait(context.Background(), "run-1", WaitTargetTerminal, time.Second, nil)
	if err == nil || !strings.Contains(err.Error(), "runtime exited") {
		t.Fatalf("Wait error = %v, want terminal diagnostic", err)
	}
	if result.GetStatus() != runv1.RunStatus_RUN_STATUS_FAILED {
		t.Fatalf("Wait status = %s, want failed", result.GetStatus())
	}
}

func TestWaitRejectsWatchEndingBeforeTarget(t *testing.T) {
	runs := &fakeRunClient{getResponses: []*runv1.GetRunResponse{{Run: &runv1.Run{
		ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_PLACED, Version: 1,
	}}}}
	control := New(runs)

	_, err := control.Wait(context.Background(), "run-1", WaitTargetRunning, time.Second, nil)
	if err == nil || !strings.Contains(err.Error(), "watch ended before reaching running") {
		t.Fatalf("Wait error = %v, want premature watch end", err)
	}
}

func TestWaitRejectsEmptyWatchResponse(t *testing.T) {
	runs := &fakeRunClient{
		getResponses: []*runv1.GetRunResponse{{Run: &runv1.Run{
			ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_PLACED, Version: 1,
		}}},
		watchResponses: []*runv1.WatchRunResponse{{}},
	}
	control := New(runs)

	_, err := control.Wait(context.Background(), "run-1", WaitTargetRunning, time.Second, nil)
	if err == nil || !strings.Contains(err.Error(), "watch run run-1 returned an empty response") {
		t.Fatalf("Wait error = %v, want empty watch response", err)
	}
}

func TestWaitTimeoutCancelsBlockedWatch(t *testing.T) {
	runs := &fakeRunClient{
		getResponses: []*runv1.GetRunResponse{{Run: &runv1.Run{
			ID: "run-1", Status: runv1.RunStatus_RUN_STATUS_PLACED, Version: 1,
		}}},
		watchBlocks: true,
	}
	control := New(runs)

	_, err := control.Wait(context.Background(), "run-1", WaitTargetRunning, time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for run run-1") {
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
	createRequest  *runv1.CreateRunRequest
	getCalls       int
	getResponses   []*runv1.GetRunResponse
	watchCalls     int
	watchRequest   *runv1.WatchRunRequest
	watchResponses []*runv1.WatchRunResponse
	watchBlocks    bool
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

func (f *fakeRunClient) WatchRun(ctx context.Context, req *runv1.WatchRunRequest, _ ...grpc.CallOption) (runv1.RunControl_WatchRunClient, error) {
	f.watchCalls++
	f.watchRequest = req
	return &fakeRunWatch{ctx: ctx, responses: f.watchResponses, block: f.watchBlocks}, nil
}

type fakeRunWatch struct {
	grpc.ClientStream
	ctx       context.Context
	responses []*runv1.WatchRunResponse
	index     int
	block     bool
}

func (f *fakeRunWatch) Recv() (*runv1.WatchRunResponse, error) {
	if f.index >= len(f.responses) {
		if f.block {
			<-f.ctx.Done()
			return nil, f.ctx.Err()
		}
		return nil, io.EOF
	}
	response := f.responses[f.index]
	f.index++
	return response, nil
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
