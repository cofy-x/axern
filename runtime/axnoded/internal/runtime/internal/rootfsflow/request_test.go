package rootfsflow

import (
	"context"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
)

type providerStub struct {
	view rootfsview.View
	err  error
}

func (p providerStub) Prepare(context.Context, string, rootfsview.Source) (rootfsview.View, error) {
	return p.view, p.err
}

func (providerStub) Remove(context.Context, string) error { return nil }

func TestPrepareRequestClonesWritableRequest(t *testing.T) {
	request := &apipb.CreateContainerRequest{Rootfs: &apipb.Rootfs{RootDir: "/image"}}

	effective, writable, err := PrepareRequest(
		context.Background(),
		providerStub{view: rootfsview.View{RootDir: "/views/alloc-1", Writable: true}},
		contract.HandlerOptions{ContainerID: "alloc-1"},
		request,
	)
	if err != nil {
		t.Fatalf("PrepareRequest() error = %v", err)
	}
	if !writable {
		t.Fatal("PrepareRequest() writable = false")
	}
	if effective == request {
		t.Fatal("PrepareRequest() mutated the shared request")
	}
	if effective.GetRootfs().GetRootDir() != "/views/alloc-1" || effective.GetRootfs().GetReadonly() {
		t.Fatalf("effective rootfs = %+v", effective.GetRootfs())
	}
	if request.GetRootfs().GetRootDir() != "/image" {
		t.Fatalf("original rootfs = %+v", request.GetRootfs())
	}
}

func TestPrepareRequestLeavesReadonlyRequestUnchanged(t *testing.T) {
	request := &apipb.CreateContainerRequest{Rootfs: &apipb.Rootfs{RootDir: "/image", Readonly: true}}

	effective, writable, err := PrepareRequest(
		context.Background(),
		providerStub{},
		contract.HandlerOptions{ContainerID: "alloc-1"},
		request,
	)
	if err != nil {
		t.Fatalf("PrepareRequest() error = %v", err)
	}
	if writable {
		t.Fatal("PrepareRequest() writable = true")
	}
	if effective != request {
		t.Fatal("PrepareRequest() cloned an unchanged request")
	}
}
