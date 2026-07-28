package api

import (
	"context"
	"testing"

	"github.com/cofy-x/axern/runtime/volumed/internal/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

func TestServerPublishesAndListsVolumes(t *testing.T) {
	manager, err := storage.NewDefaultManager(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewDefaultManager() error = %v", err)
	}
	server := NewServer(manager)
	resp, err := server.PublishVolume(context.Background(), &runtimevolumev1.PublishVolumeRequest{
		AllocationID: "alloc-1",
		RuntimeClass: "runsc",
		Volume:       resolvedVolume(),
	})
	if err != nil {
		t.Fatalf("PublishVolume() error = %v", err)
	}
	if resp.GetVolume().GetHostPath() == "" {
		t.Fatalf("PublishVolume() = %#v, want host path", resp)
	}
	list, err := server.ListPublishedVolumes(context.Background(), &runtimevolumev1.ListPublishedVolumesRequest{AllocationID: "alloc-1"})
	if err != nil {
		t.Fatalf("ListPublishedVolumes() error = %v", err)
	}
	if len(list.GetVolumes()) != 1 {
		t.Fatalf("ListPublishedVolumes() len = %d, want 1", len(list.GetVolumes()))
	}
}

func TestServerReconcileReturnsDetailedCounts(t *testing.T) {
	manager, err := storage.NewDefaultManager(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewDefaultManager() error = %v", err)
	}
	server := NewServer(manager)
	if _, err := server.PublishVolume(context.Background(), &runtimevolumev1.PublishVolumeRequest{
		AllocationID: "alloc-1",
		RuntimeClass: "runsc",
		Volume:       resolvedVolume(),
	}); err != nil {
		t.Fatalf("PublishVolume(alloc-1) error = %v", err)
	}
	other := resolvedVolume()
	other.BindingID = "binding-2"
	other.Parameters[storage.LocalParameterVolumeName] = "cache"
	if _, err := server.PublishVolume(context.Background(), &runtimevolumev1.PublishVolumeRequest{
		AllocationID: "alloc-2",
		RuntimeClass: "runsc",
		Volume:       other,
	}); err != nil {
		t.Fatalf("PublishVolume(alloc-2) error = %v", err)
	}

	resp, err := server.ReconcileVolumes(context.Background(), &runtimevolumev1.ReconcileVolumesRequest{
		ActiveAllocationIds: []string{"alloc-1"},
	})
	if err != nil {
		t.Fatalf("ReconcileVolumes() error = %v", err)
	}
	if resp.GetActiveAllocationCount() != 1 || resp.GetRetainedCount() != 1 || resp.GetUnpublishedCount() != 1 || resp.GetStaleAllocationCount() != 1 || resp.GetInvalidVolumeCount() != 0 {
		t.Fatalf("ReconcileVolumes() = %+v, want detailed counts", resp)
	}
}

func resolvedVolume() *privatestoragev1.ResolvedNodeVolume {
	return &privatestoragev1.ResolvedNodeVolume{
		ClaimID:            "claim-1",
		BackendHandle:      "claim-1",
		BindingID:          "binding-1",
		Backend:            storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		AccessMode:         storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		ConsistencyProfile: storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		Target:             "/data",
		Parameters: map[string]string{
			storage.LocalParameterNamespace:  "default",
			storage.LocalParameterServiceID:  "svc-123",
			storage.LocalParameterVolumeName: "data",
		},
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{
			SupportsRunc:  true,
			SupportsRunsc: true,
		},
	}
}
