package imageprocess

import (
	"context"
	"errors"
	"io"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type Stream interface {
	Recv() (*runtime.ProcessImageRequest, error)
	Send(*runtime.ProcessImageResponse) error
	Context() context.Context
}

func RunStream(ctx context.Context, stream Stream, session contract.Session) error {
	inputErrCh := make(chan error, 1)
	go func() {
		defer close(inputErrCh)
		for {
			req, recvErr := stream.Recv()
			if recvErr != nil {
				if errors.Is(recvErr, io.EOF) {
					inputErrCh <- session.CloseStdin()
					return
				}
				inputErrCh <- recvErr
				return
			}
			switch payload := req.Payload.(type) {
			case *runtime.ProcessImageRequest_Stdin:
				if err := session.Write(payload.Stdin); err != nil {
					inputErrCh <- err
					return
				}
			case *runtime.ProcessImageRequest_Resize:
				if err := session.Resize(payload.Resize.GetCols(), payload.Resize.GetRows()); err != nil {
					inputErrCh <- err
					return
				}
			case *runtime.ProcessImageRequest_CloseStdin:
				if payload.CloseStdin {
					if err := session.CloseStdin(); err != nil {
						inputErrCh <- err
						return
					}
				}
			case *runtime.ProcessImageRequest_Signal:
				if err := session.Signal(payload.Signal.GetSignal()); err != nil {
					inputErrCh <- err
					return
				}
			case *runtime.ProcessImageRequest_Open:
				inputErrCh <- errord.ErrInvalidArgument
				return
			default:
				inputErrCh <- errord.ErrInvalidArgument
				return
			}
		}
	}()

	outputDone := make(chan error, 1)
	go func() {
		defer close(outputDone)
		for {
			chunk, recvErr := session.Recv()
			if recvErr != nil {
				if errors.Is(recvErr, io.EOF) {
					outputDone <- nil
					return
				}
				outputDone <- recvErr
				return
			}
			switch {
			case len(chunk.Stdout) > 0:
				if err := stream.Send(&runtime.ProcessImageResponse{Payload: &runtime.ProcessImageResponse_Stdout{Stdout: chunk.Stdout}}); err != nil {
					outputDone <- err
					return
				}
			case len(chunk.Stderr) > 0:
				if err := stream.Send(&runtime.ProcessImageResponse{Payload: &runtime.ProcessImageResponse_Stderr{Stderr: chunk.Stderr}}); err != nil {
					outputDone <- err
					return
				}
			}
		}
	}()

	waitCh := make(chan struct {
		exit contract.Exit
		err  error
	}, 1)
	go func() {
		exit, waitErr := session.Wait()
		waitCh <- struct {
			exit contract.Exit
			err  error
		}{exit: exit, err: waitErr}
	}()

	var outputErr error
	select {
	case outputErr = <-outputDone:
	case inputErr := <-inputErrCh:
		if inputErr != nil {
			return inputErr
		}
		outputErr = <-outputDone
	case <-ctx.Done():
		return ctx.Err()
	}
	if outputErr != nil {
		return outputErr
	}

	select {
	case inputErr := <-inputErrCh:
		if inputErr != nil {
			return inputErr
		}
	default:
	}

	result := <-waitCh
	if result.err != nil && !contract.IsExitStatusUnavailable(result.err) {
		return result.err
	}
	message := ""
	if result.err != nil {
		message = result.err.Error()
	}
	return stream.Send(&runtime.ProcessImageResponse{Payload: &runtime.ProcessImageResponse_Exit{Exit: &runtime.ExecExit{
		ExitCode:           int32(result.exit.Status),
		Message:            message,
		ManagedProxyReport: result.exit.ManagedProxyReport,
	}}})
}
