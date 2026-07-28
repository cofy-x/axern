package storage

import (
	"context"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

type ProviderCapabilities struct {
	Backend              storagev1.VolumeBackend
	AccessModes          []storagev1.VolumeAccessMode
	ConsistencyProfiles  []storagev1.VolumeConsistencyProfile
	RuntimeCompatibility *storagev1.VolumeRuntimeCompatibility
}

type Provider interface {
	Backend() storagev1.VolumeBackend
	Capabilities() ProviderCapabilities
	Publish(ctx context.Context, allocationID string, volume *privatestoragev1.ResolvedNodeVolume) (*runtimevolumev1.PublishedVolume, error)
	Unpublish(ctx context.Context, allocationID string, volume *runtimevolumev1.PublishedVolume) error
	Delete(ctx context.Context, claimID, backendHandle string) error
	ValidatePublished(ctx context.Context, allocationID string, volume *runtimevolumev1.PublishedVolume) error
}
