package nodebridge

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AllocationAccessGrantAcceptedHeader is emitted after axnoded validates an
// allocation-scoped gateway access grant.
const AllocationAccessGrantAcceptedHeader = "x-axern-allocation-access-accepted"

// AllocationAccessGrantTokenMetadata carries the gateway-issued allocation authority
// on the private gateway-to-node hop. It is deliberately absent from public
// NodeSandbox request messages.
const AllocationAccessGrantTokenMetadata = "x-axern-allocation-access-token"

// WithAllocationAccessGrant replaces any existing outgoing grant value. Gateway
// callers must never forward a caller-supplied value to axnoded.
func WithAllocationAccessGrant(ctx context.Context, token string) context.Context {
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(AllocationAccessGrantTokenMetadata, token)
	return metadata.NewOutgoingContext(ctx, md)
}

// IsAllocationAccessGrantRejected reports whether a node rejected the allocation-scoped
// execution authority before accepting the operation.
func IsAllocationAccessGrantRejected(err error) bool {
	return err != nil && status.Code(err) == codes.Unauthenticated
}

func AllocationAccessGrantAccepted(header metadata.MD) bool {
	values := header.Get(AllocationAccessGrantAcceptedHeader)
	return len(values) == 1 && values[0] == "1"
}

// WaitAccessGrantRetry applies the bounded linear backoff shared by gateway node
// paths while remaining responsive to the request deadline.
func WaitAccessGrantRetry(ctx context.Context, failedAttempt int, baseDelay time.Duration) error {
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
