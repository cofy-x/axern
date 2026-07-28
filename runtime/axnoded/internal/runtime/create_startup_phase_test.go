package runtime

import (
	"context"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

type startupPhaseRecorderSpy struct {
	phases map[contract.StartupPhase]time.Duration
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

func TestHandlerOptionsRecordStartupPhaseNilSafe(t *testing.T) {
	contract.HandlerOptions{}.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Millisecond)
}

func TestRuncCreateContainerRecordsStartupPhases(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{
		Binary: writeFakeOCIRuntimeBinary(t, rootDir, "runc"),
	}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	handler.common.SetRuntimeRunnerBinary(writeFakeRuntimeRunnerBinary(t, rootDir))
	disableSandboxReadyWait(t, handler)
	handler.ignoreCgroups = true

	recorder := &startupPhaseRecorderSpy{}
	meta, err := handler.CreateContainer(context.Background(), newLocalCreateRequest(t), contract.HandlerOptions{
		ContainerID:           "runc-startup-test",
		StartupPhaseRecorder:  recorder,
		AdditionalAnnotations: map[string]string{"test": "true"},
	})
	if err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}
	if meta == nil {
		t.Fatal("CreateContainer() returned nil metadata")
	}
	assertRecordedStartupPhases(t, recorder, contract.StartupPhaseRuntimeBundle, contract.StartupPhaseRuntimeLaunch)
}

func TestRunscCreateContainerRecordsStartupPhases(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{
		Binary: writeFakeOCIRuntimeBinary(t, rootDir, "runsc"),
	}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	handler.common.SetRuntimeRunnerBinary(writeFakeRuntimeRunnerBinary(t, rootDir))
	disableSandboxReadyWait(t, handler)
	handler.ignoreCgroups = true

	recorder := &startupPhaseRecorderSpy{}
	meta, err := handler.CreateContainer(context.Background(), newLocalCreateRequest(t), contract.HandlerOptions{
		ContainerID:           "runsc-startup-test",
		StartupPhaseRecorder:  recorder,
		AdditionalAnnotations: map[string]string{"test": "true"},
	})
	if err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}
	if meta == nil {
		t.Fatal("CreateContainer() returned nil metadata")
	}
	assertRecordedStartupPhases(t, recorder, contract.StartupPhaseRuntimeBundle, contract.StartupPhaseRuntimeLaunch)
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
