package nodebridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestWithAllocationAccessGrantReplacesExistingOutgoingAuthority(t *testing.T) {
	t.Parallel()
	ctx := metadata.AppendToOutgoingContext(context.Background(), AllocationAccessGrantTokenMetadata, "caller-token")
	ctx = WithAllocationAccessGrant(ctx, "gateway-token")
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("outgoing metadata is missing")
	}
	if got := md.Get(AllocationAccessGrantTokenMetadata); len(got) != 1 || got[0] != "gateway-token" {
		t.Fatalf("allocation access grant metadata = %#v, want only gateway-token", got)
	}
}

func TestIsAllocationAccessGrantRejectedUsesGRPCStatus(t *testing.T) {
	t.Parallel()
	if !IsAllocationAccessGrantRejected(status.Error(codes.Unauthenticated, "rejected")) {
		t.Fatal("Unauthenticated error was not classified as a lease rejection")
	}
	if IsAllocationAccessGrantRejected(errors.New("allocation access grant is invalid")) {
		t.Fatal("unstructured error was classified as a lease rejection")
	}
	if IsAllocationAccessGrantRejected(status.Error(codes.PermissionDenied, "denied")) {
		t.Fatal("PermissionDenied error was classified as a lease rejection")
	}
}

func TestWaitAccessGrantRetryHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	err := WaitAccessGrantRetry(ctx, 1, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitAccessGrantRetry() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("WaitAccessGrantRetry() cancellation took %s", elapsed)
	}
}
