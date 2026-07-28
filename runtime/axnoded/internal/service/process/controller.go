package process

import (
	"context"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxtarget"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"go.opentelemetry.io/otel/attribute"
)

type Options struct {
	ExecTarget func(id string) (sandboxtarget.Target, error)
	Executor   *Executor
}

type Controller struct {
	execTarget func(id string) (sandboxtarget.Target, error)
	executor   *Executor
}

type ExecStreamServer interface {
	ExecStream
	Context() context.Context
}

type ProcessStreamServer interface {
	ProcessStream
	Context() context.Context
}

func NewController(options Options) *Controller {
	executor := options.Executor
	if executor == nil {
		executor = NewExecutor()
	}
	return &Controller{
		execTarget: options.ExecTarget,
		executor:   executor,
	}
}

func (c *Controller) Exec(ctx context.Context, request *runtime.ExecRequest) (*runtime.ExecResponse, error) {
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        sandboxobs.SpanExec,
		SpanAttrs:   []attribute.KeyValue{attribute.String(sdkobs.AttrAllocationID, request.GetID())},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "exec")},
		Counter:     sandboxobs.MetricExecTotal,
		Duration:    sandboxobs.MetricExecDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()
	if request.GetID() == "" || len(request.GetCommand()) == 0 {
		opErr = errord.ErrInvalidArgument
		return nil, opErr
	}

	target, err := c.resolveExecTarget(request.GetID())
	if err != nil {
		opErr = err
		return nil, err
	}
	addRuntimeMetric(op, target)

	execCtx, cancel := timeoutContext(ctx, request.GetTimeout())
	defer cancel()

	resp, err := c.executor.Exec(execCtx, processTarget(target), request)
	if err != nil {
		opErr = err
		return nil, err
	}
	return resp, nil
}

func (c *Controller) ExecStream(stream ExecStreamServer) error {
	ctx, op := sdkobs.StartOperation(stream.Context(), sdkobs.OperationConfig{
		Name:        sandboxobs.SpanExecStream,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "exec_stream")},
		Counter:     sandboxobs.MetricExecTotal,
		Duration:    sandboxobs.MetricExecDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()
	first, err := stream.Recv()
	if err != nil {
		opErr = err
		return err
	}

	open := first.GetOpen()
	if open == nil {
		opErr = errord.ErrInvalidArgument
		return opErr
	}
	op.SetAttributes(attribute.String(sdkobs.AttrAllocationID, open.GetID()))
	if open.GetID() == "" || len(open.GetCommand()) == 0 {
		opErr = errord.ErrInvalidArgument
		return opErr
	}
	target, err := c.resolveExecTarget(open.GetID())
	if err != nil {
		opErr = err
		return err
	}
	addRuntimeMetric(op, target)

	execCtx, cancel := timeoutContext(ctx, open.GetTimeout())
	defer cancel()

	session, err := c.executor.OpenExec(execCtx, processTarget(target), open)
	if err != nil {
		opErr = err
		return err
	}
	defer session.Close()

	result, err := RunExecStream(execCtx, stream, session)
	recordStreamResult(op, result)
	if err != nil {
		opErr = err
		return err
	}
	return nil
}

func (c *Controller) Process(stream ProcessStreamServer) error {
	ctx, op := sdkobs.StartOperation(stream.Context(), sdkobs.OperationConfig{
		Name:        sandboxobs.SpanExecStream,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "process")},
		Counter:     sandboxobs.MetricExecTotal,
		Duration:    sandboxobs.MetricExecDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()

	first, err := stream.Recv()
	if err != nil {
		opErr = err
		return err
	}
	open := first.GetOpen()
	if open == nil {
		opErr = errord.ErrInvalidArgument
		return opErr
	}
	op.SetAttributes(attribute.String(sdkobs.AttrAllocationID, open.GetID()))
	if open.GetID() == "" || len(open.GetCommand()) == 0 {
		opErr = errord.ErrInvalidArgument
		return opErr
	}
	target, err := c.resolveExecTarget(open.GetID())
	if err != nil {
		opErr = err
		return err
	}
	addRuntimeMetric(op, target)

	execCtx, cancel := timeoutContext(ctx, open.GetTimeout())
	defer cancel()

	session, err := c.executor.OpenProcess(execCtx, processTarget(target), open)
	if err != nil {
		opErr = err
		return err
	}
	defer session.Close()

	if err := stream.Send(&runtime.ProcessResponse{Payload: &runtime.ProcessResponse_Ready{Ready: &runtime.ProcessReady{}}}); err != nil {
		opErr = err
		return err
	}

	result, err := RunProcessStream(execCtx, stream, session)
	recordStreamResult(op, result)
	if err != nil {
		opErr = err
		return err
	}
	return nil
}

func (c *Controller) resolveExecTarget(id string) (sandboxtarget.Target, error) {
	if c == nil || c.execTarget == nil {
		return sandboxtarget.Target{}, errord.ErrInvalidContainer
	}
	return c.execTarget(id)
}

func timeoutContext(parent context.Context, timeout int64) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, time.Duration(timeout)*time.Second)
}

func processTarget(target sandboxtarget.Target) Target {
	return Target{
		ID:      target.ID,
		Labels:  target.Labels(),
		Handler: target.Handler,
	}
}

func addRuntimeMetric(op *sdkobs.Operation, target sandboxtarget.Target) {
	if runtimeClass := target.RuntimeClass(); runtimeClass != "" {
		op.AddMetricAttributes(attribute.String(sdkobs.AttrRuntime, runtimeClass))
	}
}

func recordStreamResult(op *sdkobs.Operation, result StreamResult) {
	switch result {
	case StreamResultTimeout:
		op.SetResult(sdkobs.ResultTimeout)
	case StreamResultExit:
		op.SetResult(sdkobs.ResultExit)
	}
}
