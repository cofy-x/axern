package allocation

import (
	"context"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

func (f fakeVolumePublisher) DeleteVolume(context.Context, string, storagev1.VolumeBackend, string) error {
	return nil
}

type fakeVolumePublisher struct {
	published    []*privatestoragev1.PublishedNodeVolume
	listed       []*privatestoragev1.PublishedNodeVolume
	publishCalls *int
}

func (f fakeVolumePublisher) PublishAll(context.Context, string, string, []*privatestoragev1.ResolvedNodeVolume) ([]*privatestoragev1.PublishedNodeVolume, error) {
	if f.publishCalls != nil {
		(*f.publishCalls)++
	}
	return f.published, nil
}

func (f fakeVolumePublisher) ListPublishedVolumes(context.Context, string) ([]*privatestoragev1.PublishedNodeVolume, error) {
	return f.listed, nil
}

func (f fakeVolumePublisher) UnpublishAllocation(context.Context, string) ([]*privatestoragev1.VolumeReleaseObservation, error) {
	return nil, nil
}

func (f fakeVolumePublisher) ReconcileActiveAllocations(context.Context, []string) (*runtimevolumev1.ReconcileVolumesResponse, error) {
	return &runtimevolumev1.ReconcileVolumesResponse{}, nil
}
