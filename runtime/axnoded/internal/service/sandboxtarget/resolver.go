package sandboxtarget

import (
	"context"
	"fmt"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type Runtime interface {
	Wait(context.Context, contract.HandlerOptions) (contract.Exit, error)
	KillContainer(context.Context, *runtime.SignalContainerRequest, contract.HandlerOptions) (*runtime.SignalContainerResponse, error)
	ExecContainer(context.Context, *runtime.ExecContainerRequest, contract.HandlerOptions) (*runtime.ExecContainerResponse, error)
	OpenExecSession(context.Context, *runtime.ExecSessionOpen, contract.HandlerOptions) (contract.Session, error)
	FileService() contract.FileService
}

type Options struct {
	GetContainer func(id string) (*container.Container, error)
	RunscHandler Runtime
}

type Resolver struct {
	getContainer func(id string) (*container.Container, error)
	runscHandler Runtime
}

type Target struct {
	ID        string
	Metadata  *runtime.ContainerMetadata
	Container *container.Container
	Handler   Runtime
}

func NewResolver(options Options) *Resolver {
	return &Resolver{
		getContainer: options.GetContainer,
		runscHandler: options.RunscHandler,
	}
}

func (r *Resolver) Container(id string) (Target, error) {
	if id == "" {
		return Target{}, errord.ErrInvalidArgument
	}
	if r == nil || r.getContainer == nil {
		return Target{}, fmt.Errorf("sandbox target container resolver is not configured")
	}
	if r.runscHandler == nil {
		return Target{}, fmt.Errorf("sandbox target runsc handler is not configured: %w", errord.ErrInvalidContainer)
	}
	c, err := r.getContainer(id)
	if err != nil {
		return Target{}, err
	}
	if c == nil || c.Metadata == nil {
		return Target{}, errord.ErrInvalidContainer
	}
	return Target{
		ID:        id,
		Metadata:  c.Metadata,
		Container: c,
		Handler:   r.runscHandler,
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
	return target, nil
}
