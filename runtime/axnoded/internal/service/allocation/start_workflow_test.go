package allocation

import (
	"context"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

func TestStartManagedContainerSerializesDuplicateAllocationStarts(t *testing.T) {
	rootfsDir := t.TempDir()
	createEntered := make(chan struct{})
	releaseCreate := make(chan struct{})
	handler := &runtimeSpyHandler{
		name: "runc",
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
}
