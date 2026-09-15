package sandboxd

import (
	"context"
	"errors"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func TestWaitReadyOrExitReturnsReadySuccess(t *testing.T) {
	meta := &apipb.ContainerMetadata{}

	err := WaitReadyOrExit(
		context.Background(),
		"runsc",
		"container-ready",
		"/bundle",
		meta,
		func(context.Context, string, *apipb.ContainerMetadata) error {
			return nil
		},
		func(string) (contract.Exit, bool, error) {
			t.Fatal("readExit should not be called after ready success")
			return contract.Exit{}, false, nil
		},
	)
	if err != nil {
		t.Fatalf("WaitReadyOrExit() error = %v", err)
	}
}

func TestWaitReadyOrExitReturnsExitWithoutWaitingForReadyTimeout(t *testing.T) {
	meta := &apipb.ContainerMetadata{}
	start := time.Now()

	err := WaitReadyOrExit(
		context.Background(),
		"runsc",
		"container-fast-exit",
		"/bundle",
		meta,
		func(ctx context.Context, _ string, _ *apipb.ContainerMetadata) error {
			<-ctx.Done()
			return ctx.Err()
		},
		func(string) (contract.Exit, bool, error) {
			return contract.Exit{Timestamp: time.Now(), Status: 0}, true, nil
		},
	)
	if err != nil {
		t.Fatalf("WaitReadyOrExit() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("WaitReadyOrExit() elapsed = %v, want fast exit recovery", elapsed)
	}
}

func TestWaitReadyOrExitAcceptsNonZeroExitBeforeReady(t *testing.T) {
	meta := &apipb.ContainerMetadata{}
	err := WaitReadyOrExit(
		context.Background(),
		"runsc",
		"container-fast-exit",
		"/bundle",
		meta,
		func(ctx context.Context, _ string, _ *apipb.ContainerMetadata) error {
			<-ctx.Done()
			return ctx.Err()
		},
		func(string) (contract.Exit, bool, error) {
			return contract.Exit{Timestamp: time.Now(), Status: 42}, true, nil
		},
	)
	if err != nil {
		t.Fatalf("WaitReadyOrExit() error = %v", err)
	}
}

func TestWaitReadyOrExitReturnsReadyError(t *testing.T) {
	readyErr := errors.New("ready failed")

	err := WaitReadyOrExit(
		context.Background(),
		"runsc",
		"container-ready-error",
		"/bundle",
		&apipb.ContainerMetadata{},
		func(context.Context, string, *apipb.ContainerMetadata) error {
			return readyErr
		},
		func(string) (contract.Exit, bool, error) {
			return contract.Exit{}, false, nil
		},
	)
	if !errors.Is(err, readyErr) {
		t.Fatalf("WaitReadyOrExit() error = %v, want ready error", err)
	}
}
