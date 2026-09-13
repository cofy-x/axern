package allocation

import (
	"context"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestStartupObservationDurationSinceClampsNonPositiveDuration(t *testing.T) {
	if got := startupObservationDurationSince(time.Now().Add(time.Second)); got != time.Nanosecond {
		t.Fatalf("startupObservationDurationSince() = %v, want %v", got, time.Nanosecond)
	}
}

func TestStartAllocationReservesMemoryBeforeImageOrRootfsSideEffects(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		requirements: contract.HostRequirements{Resources: []resourcemanager.ResourceName{resourcemanager.CgroupResourceName}},
	}
	fixture := newTestAllocationControllerWithResources(
		t,
		handler,
		nil,
		newRejectingTestResourceManager(resourcemanager.CgroupResourceName),
	)
	request := &runtimeapi.StartRequest{
		ContainerID: "alloc-rejected-before-side-effects",
		EnvironmentTemplate: &runtimeapi.EnvironmentTemplate{
			ID:     "runtime-rejected",
			Rootfs: &runtimeapi.RootfsConfig{Type: runtimeapi.RootfsSrcType_LOCAL, Source: &runtimeapi.RootfsConfig_Path{Path: t.TempDir()}},
		},
		Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{MemoryBytes: 256 << 20}},
	}
	if _, err := fixture.controller.startAllocation(context.Background(), request); err == nil {
		t.Fatal("startAllocation() accepted rejected node-local admission")
	}
	if handler.createCalls != 0 || len(fixture.environmentCache.List()) != 0 {
		t.Fatalf("side effects after rejected admission: runtime=%d rootfs=%d", handler.createCalls, len(fixture.environmentCache.List()))
	}
}

func TestStartAllocationPreservesFastExitStatus(t *testing.T) {
	const containerID = "alloc-fast-exit"
	releaseExit := make(chan struct{})
	handler := &runtimeSpyHandler{
		name: "runsc",
		waitFunc: func(ctx context.Context, _ contract.HandlerOptions) (contract.Exit, error) {
			select {
			case <-releaseExit:
				return contract.Exit{Status: 42, Timestamp: time.Now().UTC()}, nil
			case <-ctx.Done():
				return contract.Exit{}, ctx.Err()
			}
		},
		listStates: []*contract.UnionContainerState{{
			ID:     containerID,
			Status: contract.ContainerStatusExited,
		}},
		listHook: func() { close(releaseExit) },
	}
	fixture := newTestAllocationController(t, handler)

	response, err := fixture.controller.startAllocation(context.Background(), &runtimeapi.StartRequest{
		ContainerID: containerID,
		EnvironmentTemplate: &runtimeapi.EnvironmentTemplate{
			ID: "runtime-fast-exit",
			Rootfs: &runtimeapi.RootfsConfig{
				Type:   runtimeapi.RootfsSrcType_LOCAL,
				Source: &runtimeapi.RootfsConfig_Path{Path: t.TempDir()},
			},
			Argv: []string{"/bin/sh", "-c", "exit 42"},
		},
	})
	if err != nil {
		t.Fatalf("startAllocation() error = %v", err)
	}
	if response.GetID() != containerID {
		t.Fatalf("container id = %q, want %q", response.GetID(), containerID)
	}
	assertExactContainerExit(t, fixture, containerID, 42)
}

func TestStartAllocationSerializesDuplicateAllocationStarts(t *testing.T) {
	rootfsDir := t.TempDir()
	createEntered := make(chan struct{})
	releaseCreate := make(chan struct{})
	runtimeExited := make(chan struct{})
	handler := &runtimeSpyHandler{
		name: "runsc",
		waitFunc: func(ctx context.Context, _ contract.HandlerOptions) (contract.Exit, error) {
			select {
			case <-runtimeExited:
				return contract.Exit{Status: 0, Timestamp: time.Now().UTC()}, nil
			case <-ctx.Done():
				return contract.Exit{}, ctx.Err()
			}
		},
		listStates: []*contract.UnionContainerState{{
			ID:      "alloc-duplicate-start",
			Status:  contract.ContainerStatusRunning,
			Created: "2026-05-13T03:00:00Z",
		}},
	}
	var blockOnce sync.Once
	handler.createHook = func() {
		blockOnce.Do(func() {
			close(createEntered)
			<-releaseCreate
		})
	}
	fixture := newTestAllocationController(t, handler)
	request := &runtimeapi.StartRequest{
		ContainerID: "alloc-duplicate-start",
		EnvironmentTemplate: &runtimeapi.EnvironmentTemplate{
			ID: "runtime-duplicate-start",
			Rootfs: &runtimeapi.RootfsConfig{
				Type:   runtimeapi.RootfsSrcType_LOCAL,
				Source: &runtimeapi.RootfsConfig_Path{Path: rootfsDir},
			},
			Argv: []string{"/bin/sh"},
		},
		Mounts: []*runtimeapi.Mount{{Type: "bind", Source: rootfsDir, Target: "/data"}},
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := fixture.controller.startAllocation(context.Background(), request)
		firstDone <- err
	}()

	select {
	case <-createEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first start did not reach runtime create")
	}

	type startResult struct {
		resp *runtimeapi.StartResponse
		err  error
	}
	secondDone := make(chan startResult, 1)
	go func() {
		resp, err := fixture.controller.startAllocation(context.Background(), request)
		secondDone <- startResult{resp: resp, err: err}
	}()

	select {
	case result := <-secondDone:
		t.Fatalf("duplicate start returned before first create finished: %v", result.err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseCreate)
	if err := <-firstDone; err != nil {
		t.Fatalf("first start error = %v", err)
	}
	if got := handler.lastRequest.GetMounts(); len(got) != 1 || got[0].GetType() != "bind" || got[0].GetSource() != rootfsDir || got[0].GetTarget() != "/data" {
		t.Fatalf("runtime create mounts = %#v, want explicit allocation bind mount", got)
	}
	secondResult := <-secondDone
	if secondResult.err != nil {
		t.Fatalf("duplicate start error = %v", secondResult.err)
	}
	if got := secondResult.resp.GetID(); got != request.GetContainerID() {
		t.Fatalf("duplicate start allocation id = %q, want %q", got, request.GetContainerID())
	}
	if handler.createCalls != 1 {
		t.Fatalf("runtime create calls = %d, want 1", handler.createCalls)
	}
	close(runtimeExited)
}
