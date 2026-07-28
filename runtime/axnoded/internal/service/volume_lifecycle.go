package service

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	servicevolumes "github.com/cofy-x/axern/runtime/axnoded/internal/service/volumes"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"github.com/sirupsen/logrus"
)

func (h *sandboxService) DeleteVolume(ctx context.Context, claimID string, backend storagev1.VolumeBackend, backendHandle string) error {
	return h.nodeVolumes().Delete(ctx, claimID, backend, backendHandle)
}

func (h *sandboxService) configureVolumeCoordinator() {
	h.volumes = servicevolumes.NewCoordinator(servicevolumes.Options{
		Publisher:           h.volumeClient,
		ActiveAllocationIDs: h.activeAllocationIDs,
	})
}

func (h *sandboxService) closeVolume() {
	if h == nil || h.volumeCloser == nil {
		return
	}
	closer := h.volumeCloser
	h.volumeCloser = nil
	if err := closer.Close(); err != nil {
		logrus.Warnf("failed to close volume client: %v", err)
	}
}

func (h *sandboxService) nodeVolumes() *servicevolumes.Coordinator {
	if h == nil {
		return nil
	}
	if h.volumes != nil {
		return h.volumes
	}
	return servicevolumes.NewCoordinator(servicevolumes.Options{
		Publisher:           h.volumeClient,
		ActiveAllocationIDs: h.activeAllocationIDs,
	})
}

func (h *sandboxService) activeAllocationIDs() []string {
	if h == nil || h.containerManager == nil {
		return nil
	}
	return activeAllocationIDs(h.containerManager.List())
}

func activeAllocationIDs(containers []*container.Container) []string {
	if len(containers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(containers))
	for _, item := range containers {
		if item == nil || item.Metadata == nil {
			continue
		}
		ids = append(ids, item.Metadata.ID)
	}
	return servicevolumes.NormalizeAllocationIDs(ids)
}
