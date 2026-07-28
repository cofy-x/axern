package nodeinventory

import (
	"context"
	"errors"
	"testing"
	"time"

	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCollectVolumedInventoryMapsHealthyManager(t *testing.T) {
	reconciledAt := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	source := &AxnodedSource{
		volumeHealth: func(context.Context) (*runtimevolumev1.VolumeManagerHealth, error) {
			return &runtimevolumev1.VolumeManagerHealth{
				Status:                             runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_OK,
				PublishedVolumeCount:               2,
				LastReconcileAt:                    timestamppb.New(reconciledAt),
				LastReconcileRetainedCount:         3,
				LastReconcileUnpublishedCount:      1,
				LastReconcileActiveAllocationCount: 2,
				LastReconcileStaleAllocationCount:  1,
				LastReconcileInvalidVolumeCount:    0,
			}, nil
		},
	}
	snapshot := NewSnapshot()
	source.collectVolumedInventory(context.Background(), reconciledAt, &snapshot)

	if snapshot.Components.Volumed.Status != StatusReady || !snapshot.Components.Volumed.Reachable {
		t.Fatalf("volumed component = %+v, want ready reachable", snapshot.Components.Volumed)
	}
	if snapshot.Components.Volumed.PublishedVolumeCount != 2 ||
		snapshot.Components.Volumed.LastReconcileAt != reconciledAt ||
		snapshot.Components.Volumed.LastReconcileRetainedCount != 3 ||
		snapshot.Components.Volumed.LastReconcileUnpublishedCount != 1 ||
		snapshot.Components.Volumed.LastReconcileActiveAllocationCount != 2 ||
		snapshot.Components.Volumed.LastReconcileStaleAllocationCount != 1 ||
		snapshot.Components.Volumed.LastReconcileInvalidVolumeCount != 0 {
		t.Fatalf("volumed health fields = %+v", snapshot.Components.Volumed)
	}
}

func TestCollectVolumedInventoryMapsManagerError(t *testing.T) {
	source := &AxnodedSource{
		volumeHealth: func(context.Context) (*runtimevolumev1.VolumeManagerHealth, error) {
			return &runtimevolumev1.VolumeManagerHealth{
				Status:             runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_ERROR,
				LastReconcileError: "provider validation failed",
			}, nil
		},
	}
	snapshot := NewSnapshot()
	source.collectVolumedInventory(context.Background(), time.Now().UTC(), &snapshot)

	if snapshot.Components.Volumed.Status != StatusError ||
		snapshot.Components.Volumed.Error != "provider validation failed" {
		t.Fatalf("volumed component = %+v, want error with reconcile message", snapshot.Components.Volumed)
	}
}

func TestCollectVolumedInventoryMapsUnreachableManager(t *testing.T) {
	source := &AxnodedSource{
		volumeHealth: func(context.Context) (*runtimevolumev1.VolumeManagerHealth, error) {
			return nil, errors.New("dial volumed")
		},
	}
	snapshot := NewSnapshot()
	source.collectVolumedInventory(context.Background(), time.Now().UTC(), &snapshot)

	if snapshot.Components.Volumed.Status != StatusError ||
		snapshot.Components.Volumed.Error != "dial volumed" ||
		snapshot.Sources["volumed"].Status != StatusError {
		t.Fatalf("volumed component/source = %+v / %+v", snapshot.Components.Volumed, snapshot.Sources["volumed"])
	}
}
