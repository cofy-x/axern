package volume

import (
	"context"
	"strings"
	"testing"

	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc"
)

func TestPublishAllRejectsNilVolumeAndRollsBack(t *testing.T) {
	fake := &fakeRuntimeVolumeClient{}
	client := &Client{client: fake}

	if _, err := client.PublishAll(context.Background(), "alloc-1", "runsc", []*privatestoragev1.ResolvedNodeVolume{nil}); err == nil || !strings.Contains(err.Error(), "resolved node volume is required") {
		t.Fatalf("PublishAll() error = %v, want nil volume error", err)
	}
	if fake.publishCalls != 0 {
		t.Fatalf("PublishVolume calls = %d, want 0", fake.publishCalls)
	}
	if fake.unpublishCalls != 1 {
		t.Fatalf("UnpublishVolume calls = %d, want rollback", fake.unpublishCalls)
	}
}

func TestVolumeReleaseObservationsSkipsVolumesWithoutBindingID(t *testing.T) {
	observations := volumeReleaseObservations([]*runtimevolumev1.PublishedVolume{
		nil,
		{BindingID: ""},
		{BindingID: " binding-1 "},
	})
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one valid binding", observations)
	}
	if observations[0].GetBindingID() != "binding-1" {
		t.Fatalf("binding id = %q, want trimmed binding-1", observations[0].GetBindingID())
	}
}

func TestReconcileActiveAllocationsNormalizesIDs(t *testing.T) {
	fake := &fakeRuntimeVolumeClient{}
	client := &Client{client: fake}

	if _, err := client.ReconcileActiveAllocations(context.Background(), []string{" alloc-1 ", "", "alloc-1", "alloc-2"}); err != nil {
		t.Fatalf("ReconcileActiveAllocations() error = %v", err)
	}
	if got, want := fake.lastReconcile.GetActiveAllocationIds(), []string{"alloc-1", "alloc-2"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("active allocation ids = %#v, want %#v", got, want)
	}
}

func TestReconcileActiveAllocationsAllowsNilClient(t *testing.T) {
	var client *Client
	resp, err := client.ReconcileActiveAllocations(context.Background(), []string{"alloc-1"})
	if err != nil {
		t.Fatalf("ReconcileActiveAllocations(nil) error = %v", err)
	}
	if resp == nil {
		t.Fatal("ReconcileActiveAllocations(nil) response = nil, want empty response")
	}
}

func TestListPublishedVolumesMapsRuntimeVolumes(t *testing.T) {
	fake := &fakeRuntimeVolumeClient{
		listPublished: []*runtimevolumev1.PublishedVolume{{
			ClaimID:   "claim-1",
			BindingID: "binding-1",
			HostPath:  "/host/data",
			Target:    "/data",
			Readonly:  true,
			Options:   []string{"rbind"},
		}},
	}
	client := &Client{client: fake}

	got, err := client.ListPublishedVolumes(context.Background(), "alloc-1")
	if err != nil {
		t.Fatalf("ListPublishedVolumes() error = %v", err)
	}
	if len(got) != 1 || got[0].GetBindingID() != "binding-1" || got[0].GetTarget() != "/data" {
		t.Fatalf("published volumes = %#v, want mapped binding", got)
	}
	if fake.lastList.GetAllocationID() != "alloc-1" {
		t.Fatalf("list allocation id = %q, want alloc-1", fake.lastList.GetAllocationID())
	}
}

type fakeRuntimeVolumeClient struct {
	publishCalls   int
	unpublishCalls int
	lastReconcile  *runtimevolumev1.ReconcileVolumesRequest
	lastList       *runtimevolumev1.ListPublishedVolumesRequest
	listPublished  []*runtimevolumev1.PublishedVolume
}

func (f *fakeRuntimeVolumeClient) PublishVolume(context.Context, *runtimevolumev1.PublishVolumeRequest, ...grpc.CallOption) (*runtimevolumev1.PublishVolumeResponse, error) {
	f.publishCalls++
	return &runtimevolumev1.PublishVolumeResponse{Volume: &runtimevolumev1.PublishedVolume{}}, nil
}

func (f *fakeRuntimeVolumeClient) UnpublishVolume(context.Context, *runtimevolumev1.UnpublishVolumeRequest, ...grpc.CallOption) (*runtimevolumev1.UnpublishVolumeResponse, error) {
	f.unpublishCalls++
	return &runtimevolumev1.UnpublishVolumeResponse{}, nil
}

func (f *fakeRuntimeVolumeClient) DeleteVolume(context.Context, *runtimevolumev1.DeleteVolumeRequest, ...grpc.CallOption) (*runtimevolumev1.DeleteVolumeResponse, error) {
	return &runtimevolumev1.DeleteVolumeResponse{}, nil
}

func (f *fakeRuntimeVolumeClient) GetPublishedVolume(context.Context, *runtimevolumev1.GetPublishedVolumeRequest, ...grpc.CallOption) (*runtimevolumev1.GetPublishedVolumeResponse, error) {
	return &runtimevolumev1.GetPublishedVolumeResponse{}, nil
}

func (f *fakeRuntimeVolumeClient) ListPublishedVolumes(_ context.Context, req *runtimevolumev1.ListPublishedVolumesRequest, _ ...grpc.CallOption) (*runtimevolumev1.ListPublishedVolumesResponse, error) {
	f.lastList = req
	return &runtimevolumev1.ListPublishedVolumesResponse{Volumes: f.listPublished}, nil
}

func (f *fakeRuntimeVolumeClient) ReconcileVolumes(_ context.Context, req *runtimevolumev1.ReconcileVolumesRequest, _ ...grpc.CallOption) (*runtimevolumev1.ReconcileVolumesResponse, error) {
	f.lastReconcile = req
	return &runtimevolumev1.ReconcileVolumesResponse{}, nil
}

func (f *fakeRuntimeVolumeClient) GetVolumeManagerHealth(context.Context, *runtimevolumev1.VolumeManagerHealthRequest, ...grpc.CallOption) (*runtimevolumev1.VolumeManagerHealthResponse, error) {
	return &runtimevolumev1.VolumeManagerHealthResponse{Health: &runtimevolumev1.VolumeManagerHealth{Status: runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_OK}}, nil
}
