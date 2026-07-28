package launchflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type phaseRecorder struct {
	recorded map[contract.StartupPhase]time.Duration
}

func (r *phaseRecorder) RecordStartupPhase(phase contract.StartupPhase, duration time.Duration) {
	if r.recorded == nil {
		r.recorded = make(map[contract.StartupPhase]time.Duration)
	}
	r.recorded[phase] = duration
}

func TestRunRecordsLaunchPhaseOnSuccess(t *testing.T) {
	recorder := &phaseRecorder{}

	meta, err := Run(context.Background(), Options{
		HandlerOptions: contract.HandlerOptions{
			ContainerID:          "container-success",
			StartupPhaseRecorder: recorder,
		},
		BundlePath: "/bundle",
		Metadata:   &apipb.ContainerMetadata{ID: "container-success"},
		Start: func() (<-chan error, error) {
			ch := make(chan error)
			close(ch)
			return ch, nil
		},
		WaitStart: func(context.Context, string, <-chan error) error {
			return nil
		},
		WaitReady: func(context.Context, string, *apipb.ContainerMetadata) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if meta.GetID() != "container-success" {
		t.Fatalf("metadata id = %q, want container-success", meta.GetID())
	}
	if recorder.recorded[contract.StartupPhaseRuntimeLaunch] <= 0 {
		t.Fatalf("runtime launch phase was not recorded")
	}
}

func TestRunCleansUpOnReadyFailure(t *testing.T) {
	recorder := &phaseRecorder{}
	readyErr := errors.New("not ready")
	var cleanupReason string

	_, err := Run(context.Background(), Options{
		HandlerOptions: contract.HandlerOptions{
			ContainerID:          "container-fail",
			StartupPhaseRecorder: recorder,
		},
		BundlePath: "/bundle",
		Metadata:   &apipb.ContainerMetadata{ID: "container-fail"},
		Start: func() (<-chan error, error) {
			ch := make(chan error)
			close(ch)
			return ch, nil
		},
		WaitStart: func(context.Context, string, <-chan error) error {
			return nil
		},
		WaitReady: func(context.Context, string, *apipb.ContainerMetadata) error {
			return readyErr
		},
		Cleanup: func(reason string) {
			cleanupReason = reason
		},
	})
	if !errors.Is(err, readyErr) {
		t.Fatalf("Run() error = %v, want %v", err, readyErr)
	}
	if !strings.Contains(cleanupReason, "sandboxd ready failed: not ready") {
		t.Fatalf("cleanup reason = %q", cleanupReason)
	}
	if recorder.recorded[contract.StartupPhaseRuntimeLaunch] <= 0 {
		t.Fatalf("runtime launch phase was not recorded")
	}
}

func TestRunReportsAfterStartErrorWithoutFailing(t *testing.T) {
	afterStartErr := errors.New("persister failed")
	var observed error

	_, err := Run(context.Background(), Options{
		HandlerOptions: contract.HandlerOptions{
			ContainerID: "container-after-start",
		},
		BundlePath: "/bundle",
		Metadata:   &apipb.ContainerMetadata{ID: "container-after-start"},
		Start: func() (<-chan error, error) {
			ch := make(chan error)
			close(ch)
			return ch, nil
		},
		WaitStart: func(context.Context, string, <-chan error) error {
			return nil
		},
		AfterStart: func() error {
			return afterStartErr
		},
		OnAfterStart: func(err error) {
			observed = err
		},
		WaitReady: func(context.Context, string, *apipb.ContainerMetadata) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !errors.Is(observed, afterStartErr) {
		t.Fatalf("observed after-start error = %v, want %v", observed, afterStartErr)
	}
}
