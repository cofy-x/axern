package process

import (
	"context"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type Executor struct{}

func NewExecutor() *Executor {
	return &Executor{}
}

type Target struct {
	ID      string
	Labels  map[string]string
	Handler contract.RuntimeHandler
}

type StreamResult int

const (
	StreamResultNone StreamResult = iota
	StreamResultExit
	StreamResultTimeout
)

type ExecStream interface {
	Recv() (*runtime.ExecStreamRequest, error)
	Send(*runtime.ExecStreamResponse) error
}

type ProcessStream interface {
	Recv() (*runtime.ProcessRequest, error)
	Send(*runtime.ProcessResponse) error
}

func (e *Executor) Exec(ctx context.Context, target Target, request *runtime.ExecRequest) (*runtime.ExecResponse, error) {
	if target.Handler == nil {
		return nil, errord.ErrInvalidContainer
	}
	resp, err := target.Handler.ExecContainer(ctx, &apipb.ExecContainerRequest{
		ID:           request.GetID(),
		Command:      request.GetCommand(),
		Tty:          false,
		Envs:         startplan.KeyValuesFromStringMap(request.GetEnv()),
		Cwd:          request.GetCwd(),
		User:         request.GetUser(),
		ManagedProxy: request.GetManagedProxy(),
	}, contract.HandlerOptions{
		ContainerID:     target.ID,
		ContainerLabels: target.Labels,
	})
	if err != nil {
		return nil, err
	}
	return &runtime.ExecResponse{
		ExitCode:           resp.GetExitCode(),
		Stdout:             resp.GetStdout(),
		Stderr:             resp.GetStderr(),
		StdoutTruncated:    resp.GetStdoutTruncated(),
		StderrTruncated:    resp.GetStderrTruncated(),
		ManagedProxyReport: resp.GetManagedProxyReport(),
	}, nil
}

func (e *Executor) OpenExec(ctx context.Context, target Target, open *runtime.ExecStreamOpen) (contract.Session, error) {
	if target.Handler == nil {
		return nil, errord.ErrInvalidContainer
	}
	return target.Handler.OpenExecSession(ctx, &apipb.ExecSessionOpen{
		ID:           open.GetID(),
		Command:      open.GetCommand(),
		Tty:          open.GetTty(),
		Envs:         startplan.KeyValuesFromStringMap(open.GetEnv()),
		Cwd:          open.GetCwd(),
		User:         open.GetUser(),
		InitialSize:  open.GetInitialSize(),
		ManagedProxy: open.GetManagedProxy(),
	}, contract.HandlerOptions{
		ContainerID:     target.ID,
		ContainerLabels: target.Labels,
	})
}

func (e *Executor) OpenProcess(ctx context.Context, target Target, open *runtime.ProcessOpen) (contract.Session, error) {
	if target.Handler == nil {
		return nil, errord.ErrInvalidContainer
	}
	return target.Handler.ProcessService().OpenProcess(ctx, &apipb.ProcessOpen{
		ID:           open.GetID(),
		Command:      open.GetCommand(),
		Tty:          open.GetTty(),
		Timeout:      open.GetTimeout(),
		Env:          open.GetEnv(),
		Cwd:          open.GetCwd(),
		User:         open.GetUser(),
		ManagedProxy: open.GetManagedProxy(),
	}, contract.HandlerOptions{
		ContainerID:     target.ID,
		ContainerLabels: target.Labels,
	})
}
