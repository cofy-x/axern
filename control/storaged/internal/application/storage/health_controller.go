package storage

import (
	"context"
	"fmt"
	"time"

	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

func (c *Controller) GetVolumeBindingHealth(ctx context.Context, releasingStuckAfter time.Duration) (*privatestoragev1.VolumeBindingHealth, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	return c.store.GetVolumeBindingHealth(ctx, releasingStuckAfter)
}

func (c *Controller) GetVolumeReclaimQueueHealth(ctx context.Context) (*privatestoragev1.VolumeReclaimQueueHealth, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	return c.store.GetVolumeReclaimQueueHealth(ctx)
}
