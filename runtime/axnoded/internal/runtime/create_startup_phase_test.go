package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

type startupPhaseRecorderSpy struct {
	phases      map[contract.StartupPhase]time.Duration
	launchSteps []contract.StartupStep
}

func (s *startupPhaseRecorderSpy) RecordStartupPhase(phase contract.StartupPhase, duration time.Duration) {
	if duration <= 0 {
		return
	}
	if s.phases == nil {
		s.phases = make(map[contract.StartupPhase]time.Duration)
	}
	s.phases[phase] += duration
}

func (s *startupPhaseRecorderSpy) RecordStartupStep(phase contract.StartupPhase, step contract.StartupStep, _ time.Duration) {
	if phase == contract.StartupPhaseRuntimeLaunch {
		s.launchSteps = append(s.launchSteps, step)
	}
}

func TestHandlerOptionsRecordStartupPhaseNilSafe(t *testing.T) {
	contract.HandlerOptions{}.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Millisecond)
}

func TestRunscCreateContainerRecordsStartupPhases(t *testing.T) {
	for _, tc := range []struct {
		name     string
		readyErr error
	}{
		{name: "ready"},
		{name: "sandboxd readiness fails", readyErr: errors.New("sandboxd unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rootDir := t.TempDir()
			loader, err := runtimeoci.NewBundleLoader(filepath.Join(rootDir, "containers"))
			if err != nil {
				t.Fatalf("NewBundleLoader() error = %v", err)
			}

			handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeInstanceConfig{
				Binary: writeFakeOCIRuntimeBinary(t, rootDir, "runsc"),
			}, loader)
			if err != nil {
				t.Fatalf("NewRunscServiceHandler() error = %v", err)
			}
			handler.waitForSandboxReady = func(context.Context, string, *apipb.ContainerMetadata) error { return tc.readyErr }
			handler.ignoreCgroups = true

			recorder := &startupPhaseRecorderSpy{}
			options := contract.HandlerOptions{
				ContainerID:          "runsc-startup-test",
				StartupPhaseRecorder: recorder,
			}
			prepared, err := handler.PrepareContainer(context.Background(), newLocalCreateRequest(t), options)
			if err != nil {
				t.Fatalf("PrepareContainer() error = %v", err)
			}
			t.Cleanup(func() {
				// Join the foreground runtime and its exit checkpoint before TempDir
				// removes the state directory owned by the handler.
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := handler.DeleteContainer(ctx, &apipb.DeleteContainerRequest{}, options); err != nil {
					t.Errorf("DeleteContainer() error = %v", err)
				}
			})
			meta, err := handler.StartPreparedContainer(t.Context(), prepared, options)
			if !errors.Is(err, tc.readyErr) {
				t.Fatalf("StartPreparedContainer() error = %v, want %v", err, tc.readyErr)
			}
			if meta == nil || meta != prepared.Metadata {
				t.Fatal("StartPreparedContainer() lost prepared runtime metadata")
			}
			assertRecordedStartupPhases(t, recorder, contract.StartupPhaseRuntimeBundle, contract.StartupPhaseRuntimeLaunch)
			want := []contract.StartupStep{
				contract.StartupStepRuntimeStart, contract.StartupStepRuntimeExitMonitor, contract.StartupStepRuntimeWaitStart,
				contract.StartupStepRuntimeEnforcement, contract.StartupStepSandboxdWaitReady,
			}
			if tc.readyErr == nil {
				want = append(want, contract.StartupStepRuntimeEnforcement)
			}
			if !slices.Equal(recorder.launchSteps, want) {
				t.Fatalf("launch steps = %v, want %v", recorder.launchSteps, want)
			}
		})
	}
}

func assertRecordedStartupPhases(t *testing.T, recorder *startupPhaseRecorderSpy, phases ...contract.StartupPhase) {
	t.Helper()

	for _, phase := range phases {
		duration, ok := recorder.phases[phase]
		if !ok {
			t.Fatalf("phase %q was not recorded", phase)
		}
		if duration <= 0 {
			t.Fatalf("phase %q duration = %v, want > 0", phase, duration)
		}
	}
}
