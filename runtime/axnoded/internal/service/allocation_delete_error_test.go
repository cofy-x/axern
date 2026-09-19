package service

import (
	"context"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestAllocationDeleteGRPCErrorSeparatesSnapshotBarrierFromCleanup(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{name: "ordinary cleanup unavailable", err: errord.ErrUnavailable, want: codes.Unavailable},
		{name: "snapshot publication unavailable", err: allocation.WrapRootfsSnapshotSealingError(errord.ErrUnavailable), want: codes.Aborted},
		{name: "snapshot contract invalid", err: allocation.WrapRootfsSnapshotSealingError(errord.ErrFailedPrecondition), want: codes.FailedPrecondition},
		{name: "snapshot caller canceled", err: allocation.WrapRootfsSnapshotSealingError(context.Canceled), want: codes.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := grpcstatus.Code(allocationDeleteGRPCError(test.err)); got != test.want {
				t.Fatalf("allocationDeleteGRPCError() code = %s, want %s", got, test.want)
			}
		})
	}
}
