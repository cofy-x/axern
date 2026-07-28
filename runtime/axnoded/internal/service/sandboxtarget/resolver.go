package sandboxtarget

import (
	"fmt"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type Options struct {
	GetContainer   func(id string) (*container.Container, error)
	RuntimeHandler func(runtimeName string) (contract.RuntimeHandler, error)
}

type Resolver struct {
	getContainer   func(id string) (*container.Container, error)
	runtimeHandler func(runtimeName string) (contract.RuntimeHandler, error)
}

type Target struct {
	ID        string
	Metadata  *runtime.ContainerMetadata
	Container *container.Container
	Handler   contract.RuntimeHandler
}

func NewResolver(options Options) *Resolver {
	return &Resolver{
		getContainer:   options.GetContainer,
		runtimeHandler: options.RuntimeHandler,
	}
}

func (r *Resolver) Container(id string) (Target, error) {
	if id == "" {
		return Target{}, errord.ErrInvalidArgument
	}
	if r == nil || r.getContainer == nil {
		return Target{}, fmt.Errorf("sandbox target container resolver is not configured")
	}
	if r.runtimeHandler == nil {
		return Target{}, fmt.Errorf("sandbox target runtime resolver is not configured")
	}
	c, err := r.getContainer(id)
	if err != nil {
		return Target{}, err
	}
	if c == nil || c.Metadata == nil {
		return Target{}, errord.ErrInvalidContainer
	}
	handler, err := r.runtimeHandler(c.Metadata.GetRuntimeHandler())
	if err != nil {
		return Target{}, err
	}
	if handler == nil {
		return Target{}, fmt.Errorf("runtime %s returned nil handler: %w", c.Metadata.GetRuntimeHandler(), errord.ErrInvalidContainer)
	}
	return Target{
		ID:        id,
		Metadata:  c.Metadata,
		Container: c,
		Handler:   handler,
	}, nil
}

func (r *Resolver) Running(id string) (Target, error) {
	target, err := r.Container(id)
	if err != nil {
		return Target{}, err
	}
	if target.Container.Status == nil || target.Container.Status.Get().State() != runtime.ContainerState_CONTAINER_RUNNING {
		return Target{}, fmt.Errorf("container %s is not running: %w", id, errord.ErrFailedPrecondition)
	}
	return target, nil
}

func (r *Resolver) ExecDirect(id string) (Target, error) {
	target, err := r.Running(id)
	if err != nil {
		return Target{}, err
	}
	if !target.Handler.Capabilities().CanExecDirect {
		return Target{}, errord.ErrNotImplemented
	}
	return target, nil
}

func (t Target) Labels() map[string]string {
	if t.Metadata == nil {
		return nil
	}
	return t.Metadata.GetLabels()
}

func (t Target) RuntimeClass() string {
	if t.Metadata == nil {
		return ""
	}
	return t.Metadata.GetRuntimeHandler()
}
