package imageprocess

import (
	"context"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type Controller struct {
	orchestrator Orchestrator
}

func NewController(options Options) *Controller {
	return &Controller{
		orchestrator: NewOrchestrator(options),
	}
}

func (c *Controller) ExecImage(ctx context.Context, request *runtime.ExecImageRequest) (*runtime.ExecImageResponse, error) {
	if request.GetID() == "" || !ValidSpec(request.GetSpec()) {
		return nil, errord.ErrInvalidArgument
	}

	actor, err := c.orchestrator.CreateActor(ctx, request.GetID(), request.GetSpec())
	if err != nil {
		return nil, err
	}
	defer actor.Cleanup()

	execCtx, cancel := requestContext(ctx, request.GetSpec().GetTimeout())
	defer cancel()

	session, err := openActorProcess(execCtx, actor, request.GetSpec())
	if err != nil {
		return nil, err
	}
	defer session.Close()
	if err := session.CloseStdin(); err != nil {
		return nil, err
	}

	result, err := CollectExec(execCtx, session)
	if err != nil {
		return nil, err
	}
	return &runtime.ExecImageResponse{
		ExitCode:           int32(result.Exit.Status),
		Stdout:             result.Stdout,
		Stderr:             result.Stderr,
		StdoutTruncated:    result.StdoutTruncated,
		StderrTruncated:    result.StderrTruncated,
		ManagedProxyReport: result.Exit.ManagedProxyReport,
	}, nil
}

func (c *Controller) ProcessImage(stream Stream) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil || open.GetID() == "" || !ValidSpec(open.GetSpec()) {
		return errord.ErrInvalidArgument
	}

	actor, err := c.orchestrator.CreateActor(stream.Context(), open.GetID(), open.GetSpec())
	if err != nil {
		return err
	}
	defer actor.Cleanup()

	execCtx, cancel := requestContext(stream.Context(), open.GetSpec().GetTimeout())
	defer cancel()

	session, err := openActorProcess(execCtx, actor, open.GetSpec())
	if err != nil {
		return err
	}
	defer session.Close()

	if err := stream.Send(&runtime.ProcessImageResponse{Payload: &runtime.ProcessImageResponse_Ready{Ready: &runtime.ProcessReady{}}}); err != nil {
		return err
	}
	return RunStream(execCtx, stream, session)
}

func openActorProcess(ctx context.Context, actor *Actor, spec *runtime.ImageProcessSpec) (contract.Session, error) {
	return actor.Handler.ProcessService().OpenProcess(ctx, Open(actor.ID, spec), contract.HandlerOptions{
		ContainerID:     actor.ID,
		ContainerLabels: actor.Labels,
	})
}

func requestContext(parent context.Context, timeout int64) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, time.Duration(timeout)*time.Second)
}
