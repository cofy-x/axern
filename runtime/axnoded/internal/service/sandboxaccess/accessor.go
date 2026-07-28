package sandboxaccess

import (
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type Target struct {
	ID      string
	Labels  map[string]string
	Handler contract.RuntimeHandler
}

type Options struct {
	ResolveRunningTarget func(id string) (Target, error)
}

type Accessor struct {
	resolveRunningTarget func(id string) (Target, error)
}

func NewAccessor(options Options) *Accessor {
	return &Accessor{resolveRunningTarget: options.ResolveRunningTarget}
}

func (a *Accessor) runningTarget(id string) (Target, error) {
	if id == "" {
		return Target{}, errord.ErrInvalidArgument
	}
	if a == nil || a.resolveRunningTarget == nil {
		return Target{}, fmt.Errorf("sandbox target resolver is not configured")
	}
	target, err := a.resolveRunningTarget(id)
	if err != nil {
		return Target{}, err
	}
	if target.Handler == nil {
		return Target{}, fmt.Errorf("sandbox target %s has no runtime handler", id)
	}
	if target.ID == "" {
		target.ID = id
	}
	return target, nil
}

func handlerOptions(target Target) contract.HandlerOptions {
	return contract.HandlerOptions{
		ContainerID:     target.ID,
		ContainerLabels: target.Labels,
	}
}
