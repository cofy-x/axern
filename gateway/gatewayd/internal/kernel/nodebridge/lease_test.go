package nodebridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsExecutionLeaseRejectedUsesGRPCStatus(t *testing.T) {
	t.Parallel()
	if !IsExecutionLeaseRejected(status.Error(codes.Unauthenticated, "rejected")) {
		t.Fatal("Unauthenticated error was not classified as a lease rejection")
	}
	if IsExecutionLeaseRejected(errors.New("execution lease is invalid")) {
		t.Fatal("unstructured error was classified as a lease rejection")
	}
	if IsExecutionLeaseRejected(status.Error(codes.PermissionDenied, "denied")) {
		t.Fatal("PermissionDenied error was classified as a lease rejection")
	}
}

func TestWaitLeaseRetryHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	err := WaitLeaseRetry(ctx, 1, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitLeaseRetry() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("WaitLeaseRetry() cancellation took %s", elapsed)
	}
}
