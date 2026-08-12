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
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

func TestStartManagedContainerReservesMemoryBeforeVolumeImageOrRootfsSideEffects(t *testing.T) {
	publishCalls := 0
	handler := &runtimeSpyHandler{
		name:         "runsc",
		requirements: contract.RuntimeRequirements{Resources: []resourcemanager.ResourceName{resourcemanager.CgroupResourceName}},
	}
	fixture := newTestAllocationControllerWithResources(
		t,
		map[string]contract.RuntimeHandler{"runsc": handler},
		nil,
		fakeVolumePublisher{publishCalls: &publishCalls},
		newRejectingTestResourceManager(resourcemanager.CgroupResourceName),
	)
	request := &runtimeapi.StartRequest{
		ContainerID: "alloc-rejected-before-side-effects",
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID: "runtime-rejected", Sandbox: "runsc",
			Rootfs: &runtimeapi.RootfsConfig{Type: runtimeapi.RootfsSrcType_LOCAL, Source: &runtimeapi.RootfsConfig_Path{Path: t.TempDir()}},
		},
		Resources:   &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{MemoryBytes: 256 << 20}},
		NodeVolumes: []*privatestoragev1.ResolvedNodeVolume{{ClaimID: "default/data", Target: "/data"}},
	}
	if _, err := fixture.controller.startManagedContainer(context.Background(), request); err == nil {
		t.Fatal("startManagedContainer() accepted rejected node-local admission")
	}
	if publishCalls != 0 || handler.createCalls != 0 || len(fixture.lrtManager.List()) != 0 {
		t.Fatalf("side effects after rejected admission: volume=%d runtime=%d rootfs=%d", publishCalls, handler.createCalls, len(fixture.lrtManager.List()))
	}
}

func TestStartManagedContainerPreservesFastExitStatus(t *testing.T) {
	const containerID = "alloc-managed-fast-exit"
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
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})

	response, err := fixture.controller.startManagedContainer(context.Background(), &runtimeapi.StartRequest{
		ContainerID: containerID,
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID:      "runtime-managed-fast-exit",
			Sandbox: "runsc",
			Rootfs: &runtimeapi.RootfsConfig{
				Type:   runtimeapi.RootfsSrcType_LOCAL,
				Source: &runtimeapi.RootfsConfig_Path{Path: t.TempDir()},
			},
			Command: []string{"/bin/sh", "-c", "exit 42"},
		},
	})
	if err != nil {
		t.Fatalf("startManagedContainer() error = %v", err)
	}
	if response.GetID() != containerID {
		t.Fatalf("container id = %q, want %q", response.GetID(), containerID)
	}
	assertExactContainerExit(t, fixture, containerID, 42)
}

func TestStartManagedContainerSerializesDuplicateAllocationStarts(t *testing.T) {
	rootfsDir := t.TempDir()
	createEntered := make(chan struct{})
	releaseCreate := make(chan struct{})
	runtimeExited := make(chan struct{})
	handler := &runtimeSpyHandler{
		name: "runc",
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
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{"runc": handler}, nil, fakeVolumePublisher{
		published: []*privatestoragev1.PublishedNodeVolume{{
			ClaimID:   "default/svc/data",
			BindingID: "binding-1",
			HostPath:  rootfsDir,
			Target:    "/data",
		}},
		listed: []*privatestoragev1.PublishedNodeVolume{{
			ClaimID:   "default/svc/data",
			BindingID: "binding-1",
			HostPath:  rootfsDir,
			Target:    "/data",
		}},
	})
	request := &runtimeapi.StartRequest{
		ContainerID: "alloc-duplicate-start",
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID:      "runtime-duplicate-start",
			Sandbox: "runc",
			Rootfs: &runtimeapi.RootfsConfig{
				Type:   runtimeapi.RootfsSrcType_LOCAL,
				Source: &runtimeapi.RootfsConfig_Path{Path: rootfsDir},
			},
			Command: []string{"/bin/sh"},
		},
		NodeVolumes: []*privatestoragev1.ResolvedNodeVolume{{
			ClaimID: "default/svc/data",
			Target:  "/data",
		}},
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := fixture.controller.startManagedContainer(context.Background(), request)
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
		resp, err := fixture.controller.startManagedContainer(context.Background(), request)
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
		t.Fatalf("runtime create mounts = %#v, want explicit node volume bind mount", got)
	}
	secondResult := <-secondDone
	if secondResult.err != nil {
		t.Fatalf("duplicate start error = %v", secondResult.err)
	}
	if got := secondResult.resp.GetPublishedVolumes(); len(got) != 1 || got[0].GetBindingID() != "binding-1" {
		t.Fatalf("duplicate start published volumes = %#v, want binding-1", got)
	}
	if handler.createCalls != 1 {
		t.Fatalf("runtime create calls = %d, want 1", handler.createCalls)
	}
	close(runtimeExited)
}
