package process

import (
	"context"
	"sync"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type sessionInputGate struct {
	mu      sync.Mutex
	session contract.Session
	stopped bool
}

func newSessionInputGate(session contract.Session) *sessionInputGate {
	return &sessionInputGate{session: session}
}

func (g *sessionInputGate) stop() {
	g.mu.Lock()
	g.stopped = true
	g.mu.Unlock()
}

func (g *sessionInputGate) apply(action func(contract.Session) error) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return false, nil
	}
	return true, action(g.session)
}

func waitForStreamOutput(ctx context.Context, inputErrCh <-chan error, outputDone <-chan error) (StreamResult, error) {
	var outputErr error
	select {
	case outputErr = <-outputDone:
	case inputErr := <-inputErrCh:
		if inputErr != nil {
			return StreamResultNone, inputErr
		}
		select {
		case outputErr = <-outputDone:
		case <-ctx.Done():
			return StreamResultTimeout, ctx.Err()
		}
	case <-ctx.Done():
		return StreamResultTimeout, ctx.Err()
	}
	if outputErr != nil {
		return StreamResultNone, outputErr
	}

	select {
	case inputErr := <-inputErrCh:
		if inputErr != nil {
			return StreamResultNone, inputErr
		}
	default:
	}
	return StreamResultNone, nil
}

type waitResult struct {
	exit contract.Exit
	err  error
}

func waitForSession(session contract.Session) <-chan waitResult {
	waitCh := make(chan waitResult, 1)
	go func() {
		exit, waitErr := session.Wait()
		waitCh <- waitResult{exit: exit, err: waitErr}
	}()
	return waitCh
}

func waitForSessionExit(ctx context.Context, waitCh <-chan waitResult) (waitResult, error) {
	select {
	case result := <-waitCh:
		return result, nil
	case <-ctx.Done():
		return waitResult{}, ctx.Err()
	}
}

func sendExecExit(stream ExecStream, result waitResult) (StreamResult, error) {
	if result.err != nil && !contract.IsExitStatusUnavailable(result.err) {
		return StreamResultNone, result.err
	}
	message := ""
	if result.err != nil {
		message = result.err.Error()
	}
	if err := stream.Send(&runtime.ExecStreamResponse{Payload: &runtime.ExecStreamResponse_Exit{Exit: &runtime.ExecExit{
		ExitCode:           int32(result.exit.Status),
		Message:            message,
		ManagedProxyReport: result.exit.ManagedProxyReport,
	}}}); err != nil {
		return StreamResultNone, err
	}
	return StreamResultExit, nil
}

func sendProcessExit(stream ProcessStream, result waitResult) (StreamResult, error) {
	if result.err != nil && !contract.IsExitStatusUnavailable(result.err) {
		return StreamResultNone, result.err
	}
	message := ""
	if result.err != nil {
		message = result.err.Error()
	}
	if err := stream.Send(&runtime.ProcessResponse{Payload: &runtime.ProcessResponse_Exit{Exit: &runtime.ExecExit{
		ExitCode:           int32(result.exit.Status),
		Message:            message,
		ManagedProxyReport: result.exit.ManagedProxyReport,
	}}}); err != nil {
		return StreamResultNone, err
	}
	return StreamResultExit, nil
}
