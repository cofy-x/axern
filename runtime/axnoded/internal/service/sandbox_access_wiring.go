package service

import (
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxaccess"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxtarget"
)

func (h *sandboxService) configureSandboxTargets() {
	h.sandboxTargets = sandboxtarget.NewResolver(sandboxtarget.Options{
		GetContainer: func(id string) (*container.Container, error) {
			if h.containerManager == nil {
				return nil, fmt.Errorf("container manager unavailable")
			}
			return h.containerManager.Get(id)
		},
		RuntimeHandler: h.runtimeHandler,
	})
}

func (h *sandboxService) sandboxTargetResolver() *sandboxtarget.Resolver {
	if h.sandboxTargets != nil {
		return h.sandboxTargets
	}
	h.configureSandboxTargets()
	return h.sandboxTargets
}

func (h *sandboxService) configureSandboxAccess() {
	h.sandboxAccess = sandboxaccess.NewAccessor(sandboxaccess.Options{
		ResolveRunningTarget: h.resolveSandboxAccessTarget,
	})
}

func (h *sandboxService) sandboxAccessor() *sandboxaccess.Accessor {
	if h == nil {
		return nil
	}
	if h.sandboxAccess != nil {
		return h.sandboxAccess
	}
	return sandboxaccess.NewAccessor(sandboxaccess.Options{
		ResolveRunningTarget: h.resolveSandboxAccessTarget,
	})
}

func (h *sandboxService) resolveSandboxAccessTarget(id string) (sandboxaccess.Target, error) {
	target, err := h.sandboxTargetResolver().Running(id)
	if err != nil {
		return sandboxaccess.Target{}, err
	}
	return sandboxaccess.Target{
		ID:      id,
		Labels:  target.Labels(),
		Handler: target.Handler,
	}, nil
}
