package process

import (
	"context"
	"errors"
	"io"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func RunProcessStream(ctx context.Context, stream ProcessStream, session contract.Session) (StreamResult, error) {
	input := newSessionInputGate(session)
	defer input.stop()
	inputErrCh := make(chan error, 1)
	go pumpProcessInput(stream, input, inputErrCh)

	outputDone := make(chan error, 1)
	go pumpProcessOutput(stream, session, outputDone)

	waitCh := waitForSession(session)

	if result, err := waitForStreamOutput(ctx, inputErrCh, outputDone); err != nil {
		return result, err
	}
	waitResult, err := waitForSessionExit(ctx, waitCh)
	if err != nil {
		return StreamResultTimeout, err
	}
	return sendProcessExit(stream, waitResult)
}

func pumpProcessInput(stream ProcessStream, input *sessionInputGate, result chan<- error) {
	defer close(result)
	for {
		req, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				_, err := input.apply(func(session contract.Session) error { return session.CloseStdin() })
				result <- err
				return
			}
			result <- recvErr
			return
		}
		switch payload := req.Payload.(type) {
		case *runtime.ProcessRequest_Stdin:
			active, err := input.apply(func(session contract.Session) error { return session.Write(payload.Stdin) })
			if !active || err != nil {
				result <- err
				return
			}
		case *runtime.ProcessRequest_Resize:
			active, err := input.apply(func(session contract.Session) error {
				return session.Resize(payload.Resize.GetCols(), payload.Resize.GetRows())
			})
			if !active || err != nil {
				result <- err
				return
			}
		case *runtime.ProcessRequest_CloseStdin:
			if payload.CloseStdin {
				active, err := input.apply(func(session contract.Session) error { return session.CloseStdin() })
				if !active || err != nil {
					result <- err
					return
				}
			}
		case *runtime.ProcessRequest_Signal:
			active, err := input.apply(func(session contract.Session) error {
				return session.Signal(payload.Signal.GetSignal())
			})
			if !active || err != nil {
				result <- err
				return
			}
		case *runtime.ProcessRequest_Open:
			result <- errord.ErrInvalidArgument
			return
		default:
			result <- errord.ErrInvalidArgument
			return
		}
	}
}

func pumpProcessOutput(stream ProcessStream, session contract.Session, result chan<- error) {
	defer close(result)
	for {
		chunk, recvErr := session.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				result <- nil
				return
			}
			result <- recvErr
			return
		}
		switch {
		case len(chunk.Stdout) > 0:
			if err := stream.Send(&runtime.ProcessResponse{Payload: &runtime.ProcessResponse_Stdout{Stdout: chunk.Stdout}}); err != nil {
				result <- err
				return
			}
		case len(chunk.Stderr) > 0:
			if err := stream.Send(&runtime.ProcessResponse{Payload: &runtime.ProcessResponse_Stderr{Stderr: chunk.Stderr}}); err != nil {
				result <- err
				return
			}
		}
	}
}
