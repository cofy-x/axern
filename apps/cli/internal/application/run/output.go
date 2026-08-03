package run

import (
	"context"
	"errors"
	"io"
	"time"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type OutputClient interface {
	ReadOutput(context.Context, *nodesandboxv1.ReadOutputRequest, ...grpc.CallOption) (nodesandboxv1.NodeSandbox_ReadOutputClient, error)
}

type OutputEvent struct {
	Stream     nodesandboxv1.OutputStream
	Data       []byte
	NextCursor string
	Terminal   bool
	Truncated  bool
	ObservedAt int64
}

func ReadOutput(ctx context.Context, client OutputClient, allocationID, cursor string, follow bool, consume func(OutputEvent) error) (string, error) {
	retryDelay := 100 * time.Millisecond
	var notFoundSince time.Time
	for {
		stream, err := client.ReadOutput(ctx, &nodesandboxv1.ReadOutputRequest{AllocationID: allocationID, Cursor: cursor, Follow: follow})
		if err == nil {
			for {
				response, recvErr := stream.Recv()
				if errors.Is(recvErr, io.EOF) {
					return cursor, nil
				}
				if recvErr != nil {
					err = recvErr
					break
				}
				cursor = response.GetNextCursor()
				retryDelay = 100 * time.Millisecond
				notFoundSince = time.Time{}
				if consume != nil {
					if err := consume(OutputEvent{Stream: response.GetStream(), Data: append([]byte(nil), response.GetData()...), NextCursor: cursor, Terminal: response.GetTerminal(), Truncated: response.GetTruncated(), ObservedAt: response.GetObservedAtUnixMilli()}); err != nil {
						return cursor, err
					}
				}
			}
		}
		if !follow || !retryableOutputError(err) {
			return cursor, err
		}
		if grpcstatus.Code(err) == codes.NotFound {
			if notFoundSince.IsZero() {
				notFoundSince = time.Now()
			}
			if time.Since(notFoundSince) >= 30*time.Second {
				return cursor, err
			}
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return cursor, ctx.Err()
		case <-timer.C:
		}
		if retryDelay < 2*time.Second {
			retryDelay *= 2
		}
	}
}

func retryableOutputError(err error) bool {
	switch grpcstatus.Code(err) {
	case codes.NotFound, codes.Unavailable, codes.ResourceExhausted, codes.Aborted:
		return true
	default:
		return false
	}
}
