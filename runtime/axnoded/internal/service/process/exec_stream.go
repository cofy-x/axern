package process

import (
	"context"
	"errors"
	"io"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func RunExecStream(ctx context.Context, stream ExecStream, session contract.Session) (StreamResult, error) {
	input := newSessionInputGate(session)
	defer input.stop()
	inputErrCh := make(chan error, 1)
	go pumpExecInput(stream, input, inputErrCh)

	outputDone := make(chan error, 1)
	go pumpExecOutput(stream, session, outputDone)

	waitCh := waitForSession(session)

	if result, err := waitForStreamOutput(ctx, inputErrCh, outputDone); err != nil {
		return result, err
	}
	waitResult, err := waitForSessionExit(ctx, waitCh)
	if err != nil {
		return StreamResultTimeout, err
	}
	return sendExecExit(stream, waitResult)
}

func pumpExecInput(stream ExecStream, input *sessionInputGate, result chan<- error) {
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
		case *runtime.ExecStreamRequest_Stdin:
			active, err := input.apply(func(session contract.Session) error { return session.Write(payload.Stdin) })
			if !active || err != nil {
				result <- err
				return
			}
		case *runtime.ExecStreamRequest_Resize:
			active, err := input.apply(func(session contract.Session) error {
				return session.Resize(payload.Resize.GetCols(), payload.Resize.GetRows())
			})
			if !active || err != nil {
				result <- err
				return
			}
		case *runtime.ExecStreamRequest_CloseStdin:
			if payload.CloseStdin {
				active, err := input.apply(func(session contract.Session) error { return session.CloseStdin() })
				if !active || err != nil {
					result <- err
					return
				}
			}
		case *runtime.ExecStreamRequest_Open:
			result <- errord.ErrInvalidArgument
			return
		default:
			result <- errord.ErrInvalidArgument
			return
		}
	}
}

func pumpExecOutput(stream ExecStream, session contract.Session, result chan<- error) {
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
			if err := stream.Send(&runtime.ExecStreamResponse{Payload: &runtime.ExecStreamResponse_Stdout{Stdout: chunk.Stdout}}); err != nil {
				result <- err
				return
			}
		case len(chunk.Stderr) > 0:
			if err := stream.Send(&runtime.ExecStreamResponse{Payload: &runtime.ExecStreamResponse_Stderr{Stderr: chunk.Stderr}}); err != nil {
				result <- err
				return
			}
		}
	}
}
