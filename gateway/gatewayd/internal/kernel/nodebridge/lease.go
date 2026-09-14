package nodebridge

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ExecutionLeaseAcceptedHeader is the internal gRPC acknowledgement emitted
// after axnoded validates an allocation-scoped execution lease.
const ExecutionLeaseAcceptedHeader = "x-axern-execution-lease-accepted"

// ExecutionLeaseTokenMetadata carries the gateway-issued allocation authority
// on the private gateway-to-node hop. It is deliberately absent from public
// NodeSandbox request messages.
const ExecutionLeaseTokenMetadata = "x-axern-execution-lease-token"

// WithExecutionLease replaces any existing outgoing lease value. Gateway
// callers must never forward a caller-supplied value to axnoded.
func WithExecutionLease(ctx context.Context, token string) context.Context {
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(ExecutionLeaseTokenMetadata, token)
	return metadata.NewOutgoingContext(ctx, md)
}

// IsExecutionLeaseRejected reports whether a node rejected the allocation-scoped
// execution authority before accepting the operation.
func IsExecutionLeaseRejected(err error) bool {
	return err != nil && status.Code(err) == codes.Unauthenticated
}

func ExecutionLeaseAccepted(header metadata.MD) bool {
	values := header.Get(ExecutionLeaseAcceptedHeader)
	return len(values) == 1 && values[0] == "1"
}

// WaitLeaseRetry applies the bounded linear backoff shared by gateway node
// paths while remaining responsive to the request deadline.
func WaitLeaseRetry(ctx context.Context, failedAttempt int, baseDelay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if failedAttempt <= 0 || baseDelay <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(failedAttempt) * baseDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
