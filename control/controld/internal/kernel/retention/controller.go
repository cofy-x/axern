package retention

import (
	"context"
	"time"
)

type Store interface {
	Cleanup(ctx context.Context, cfg Config, now time.Time) (Result, error)
}

type Controller struct {
	store Store
	cfg   Config
}

func NewController(store Store, cfg Config) *Controller {
	return &Controller{store: store, cfg: NormalizeConfig(cfg)}
}

func (c *Controller) Config() Config {
	if c == nil {
		return DefaultConfig()
	}
	return c.cfg
}

func (c *Controller) Cleanup(ctx context.Context, now time.Time) (Result, error) {
	start := time.Now()
	if c == nil || c.store == nil || !c.cfg.Enabled {
		return Result{Skipped: true, Duration: time.Since(start)}, nil
	}
	result, err := c.store.Cleanup(ctx, c.cfg, now)
	result.Duration = time.Since(start)
	return result, err
}
