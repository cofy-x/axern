package storage

import (
	"context"
	"fmt"
	"strings"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Controller) GetVolumeClass(ctx context.Context, name string) (*storagev1.VolumeClass, bool, error) {
	if c == nil || c.store == nil {
		return nil, false, fmt.Errorf("storage controller store is required")
	}
	return c.store.GetVolumeClass(ctx, strings.TrimSpace(name))
}

func (c *Controller) ListVolumeClasses(ctx context.Context) ([]*storagev1.VolumeClass, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	return c.store.ListVolumeClasses(ctx)
}

func (c *Controller) CreateVolumeClass(ctx context.Context, req *storagev1.CreateVolumeClassRequest) (*storagev1.VolumeClass, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	params := kernel.VolumeClassCreate{
		Name:                 strings.TrimSpace(req.GetName()),
		Backend:              req.GetBackend(),
		AccessModes:          append([]storagev1.VolumeAccessMode(nil), req.GetAccessModes()...),
		DefaultReclaimPolicy: req.GetDefaultReclaimPolicy(),
		ConsistencyProfile:   req.GetConsistencyProfile(),
		RuntimeCompatibility: cloneRuntimeCompatibility(req.GetRuntimeCompatibility()),
		Parameters:           cloneStringMap(req.GetParameters()),
	}
	if err := kernel.ValidateVolumeClassCreate(params); err != nil {
		return nil, err
	}
	now := timestamppb.New(c.now().UTC())
	return c.store.CreateVolumeClass(ctx, &storagev1.VolumeClass{
		Name:                 params.Name,
		Backend:              params.Backend,
		AccessModes:          params.AccessModes,
		DefaultReclaimPolicy: params.DefaultReclaimPolicy,
		ConsistencyProfile:   params.ConsistencyProfile,
		RuntimeCompatibility: params.RuntimeCompatibility,
		Parameters:           params.Parameters,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
}
