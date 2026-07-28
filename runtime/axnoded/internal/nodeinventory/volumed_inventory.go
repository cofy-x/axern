package nodeinventory

import (
	"context"
	"fmt"
	"strings"
	"time"

	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
)

func (s *AxnodedSource) collectVolumedInventory(ctx context.Context, now time.Time, snapshot *NodeInventorySnapshot) {
	if s.volumeHealth == nil {
		snapshot.Components.Volumed.Status = StatusDisabled
		return
	}
	health, err := s.volumeHealth(ctx)
	if err != nil {
		snapshot.Sources["volumed"] = errorSource(err)
		snapshot.Components.Volumed.Status = StatusError
		snapshot.Components.Volumed.Error = err.Error()
		return
	}
	snapshot.Sources["volumed"] = readySource(now)
	snapshot.Components.Volumed = volumedComponentInventory(health)
}

func volumedComponentInventory(health *runtimevolumev1.VolumeManagerHealth) VolumedComponentInventory {
	if health == nil {
		return VolumedComponentInventory{Status: StatusError, Error: "volumed health is empty"}
	}
	status := health.GetStatus()
	out := VolumedComponentInventory{
		Status:                             StatusReady,
		Reachable:                          true,
		PublishedVolumeCount:               int(health.GetPublishedVolumeCount()),
		LastReconcileError:                 strings.TrimSpace(health.GetLastReconcileError()),
		LastReconcileRetainedCount:         int(health.GetLastReconcileRetainedCount()),
		LastReconcileUnpublishedCount:      int(health.GetLastReconcileUnpublishedCount()),
		LastReconcileActiveAllocationCount: int(health.GetLastReconcileActiveAllocationCount()),
		LastReconcileStaleAllocationCount:  int(health.GetLastReconcileStaleAllocationCount()),
		LastReconcileInvalidVolumeCount:    int(health.GetLastReconcileInvalidVolumeCount()),
	}
	if health.GetLastReconcileAt() != nil && health.GetLastReconcileAt().IsValid() {
		out.LastReconcileAt = health.GetLastReconcileAt().AsTime()
	}
	if status == runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_DISABLED {
		out.Status = StatusDisabled
		out.Reachable = false
		return out
	}
	if status == runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_ERROR || out.LastReconcileError != "" {
		out.Status = StatusError
		if out.LastReconcileError == "" {
			out.Error = fmt.Sprintf("volumed status %s", status)
		} else {
			out.Error = out.LastReconcileError
		}
	}
	return out
}
