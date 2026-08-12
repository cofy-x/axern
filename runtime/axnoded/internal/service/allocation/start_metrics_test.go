package allocation

import (
	"context"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type countingRootfsMounter struct {
	resolveCalls int
}

func (m *countingRootfsMounter) Resolve(cfg langrtmanager.RootfsConfig) (langrtmanager.RootfsConfig, error) {
	m.resolveCalls++
	return cfg, nil
}

func (m *countingRootfsMounter) Reconcile([]string) error { return nil }

func (m *countingRootfsMounter) Mount(cfg langrtmanager.RootfsConfig) (*langrtmanager.MountResult, error) {
	mount, err := langrtmanager.DescribeLocalRootfs(cfg.Path)
	return &langrtmanager.MountResult{Path: cfg.Path, ImmutableMount: mount}, err
}

func (m *countingRootfsMounter) Umount(langrtmanager.RootfsConfig) error {
	return nil
}

type fakeStartMetricSink struct {
	starts  []startSample
	phases  []phaseMetricSample
	steps   []stepMetricSample
	results []startLabelSample
}

type startLabelSample struct {
	StartClass string
	Runtime    string
	RootfsType string
	Result     string
}

type startSample struct {
	startLabelSample
	Duration time.Duration
}

type phaseMetricSample struct {
	startLabelSample
	Phase    contract.StartupPhase
	Duration time.Duration
}

type stepMetricSample struct {
	startLabelSample
	Phase    contract.StartupPhase
	Step     contract.StartupStep
	Duration time.Duration
}

func (f *fakeStartMetricSink) RecordStartDuration(startClass, runtime, rootfsType, result string, duration time.Duration) {
	f.starts = append(f.starts, startSample{
		startLabelSample: startLabelSample{
			StartClass: startClass,
			Runtime:    runtime,
			RootfsType: rootfsType,
			Result:     result,
		},
		Duration: duration,
	})
}

func (f *fakeStartMetricSink) RecordStartPhaseDuration(phase contract.StartupPhase, startClass, runtime, rootfsType, result string, duration time.Duration) {
	f.phases = append(f.phases, phaseMetricSample{
		startLabelSample: startLabelSample{
			StartClass: startClass,
			Runtime:    runtime,
			RootfsType: rootfsType,
			Result:     result,
		},
		Phase:    phase,
		Duration: duration,
	})
}

func (f *fakeStartMetricSink) RecordStartStepDuration(phase contract.StartupPhase, step contract.StartupStep, startClass, runtime, rootfsType, result string, duration time.Duration) {
	f.steps = append(f.steps, stepMetricSample{
		startLabelSample: startLabelSample{
			StartClass: startClass,
			Runtime:    runtime,
			RootfsType: rootfsType,
			Result:     result,
		},
		Phase:    phase,
		Step:     step,
		Duration: duration,
	})
}

func (f *fakeStartMetricSink) RecordStartResult(startClass, runtime, rootfsType, result string) {
	f.results = append(f.results, startLabelSample{
		StartClass: startClass,
		Runtime:    runtime,
		RootfsType: rootfsType,
		Result:     result,
	})
}

func TestEnsureLangRuntimeSummaryWarmAndCold(t *testing.T) {
	rootfsDir := t.TempDir()
	fixture := newTestAllocationController(t, nil)
	mounter := &countingRootfsMounter{}
	manager := langrtmanager.NewLanguageRuntimeManager(mounter)
	fixture.controller.lrtManager = manager
	fixture.lrtManager = manager
	functionRuntime := &runtimeapi.RuntimeTemplate{
		ID:      "start-metrics-runtime",
		Sandbox: "runsc",
		Rootfs: &runtimeapi.RootfsConfig{
			Type: runtimeapi.RootfsSrcType_LOCAL,
			Source: &runtimeapi.RootfsConfig_Path{
				Path: rootfsDir,
			},
		},
		Command: []string{"/bin/sh"},
	}

	lrt, first, err := fixture.controller.ensureLangRuntime(t.Context(), functionRuntime)
	if err != nil {
		t.Fatalf("ensureLangRuntime() first call error = %v", err)
	}
	if lrt == nil {
		t.Fatal("ensureLangRuntime() first call returned nil runtime")
	}
	if first.RuntimeReused {
		t.Fatalf("first ensureLangRuntime() reused = true, want false")
	}
	if first.StartClass() != contract.StartupClassCold {
		t.Fatalf("first start class = %q, want %q", first.StartClass(), contract.StartupClassCold)
	}
	if first.RootfsType != contract.StartupRootfsTypeLocal {
		t.Fatalf("first rootfs type = %q, want %q", first.RootfsType, contract.StartupRootfsTypeLocal)
	}
	if first.RootfsPrepareTime <= 0 {
		t.Fatalf("first rootfs prepare duration = %v, want > 0", first.RootfsPrepareTime)
	}
	assertStartupSteps(t, first.Steps,
		contract.StartupStepRootfsResolve,
		contract.StartupStepRootfsCacheLookup,
		contract.StartupStepRootfsMount,
		contract.StartupStepRootfsActiveRef,
	)
	resolveCallsAfterColdStart := mounter.resolveCalls
	if resolveCallsAfterColdStart == 0 {
		t.Fatal("cold start did not resolve rootfs config")
	}

	lrtAgain, second, err := fixture.controller.ensureLangRuntime(t.Context(), functionRuntime)
	if err != nil {
		t.Fatalf("ensureLangRuntime() second call error = %v", err)
	}
	if lrtAgain != lrt {
		t.Fatal("ensureLangRuntime() second call returned different runtime")
	}
	if !second.RuntimeReused {
		t.Fatalf("second ensureLangRuntime() reused = false, want true")
	}
	if second.StartClass() != contract.StartupClassWarm {
		t.Fatalf("second start class = %q, want %q", second.StartClass(), contract.StartupClassWarm)
	}
	if second.RootfsPrepareTime != 0 {
		t.Fatalf("second rootfs prepare duration = %v, want 0", second.RootfsPrepareTime)
	}
	assertStartupSteps(t, second.Steps)
	if mounter.resolveCalls != resolveCallsAfterColdStart {
		t.Fatalf("warm start resolve calls = %d, want unchanged at %d", mounter.resolveCalls, resolveCallsAfterColdStart)
	}
}

func TestEnsureLangRuntimeDriftedSpecReplacesRetainedRuntime(t *testing.T) {
	rootfsDirA := t.TempDir()
	rootfsDirB := t.TempDir()
	fixture := newTestAllocationController(t, nil)
	fixture.lrtManager.ConfigureRetention(time.Minute, 8)

	firstRuntime := &runtimeapi.RuntimeTemplate{
		ID:      "start-metrics-drift-runtime",
		Sandbox: "runsc",
		Rootfs: &runtimeapi.RootfsConfig{
			Type:   runtimeapi.RootfsSrcType_LOCAL,
			Source: &runtimeapi.RootfsConfig_Path{Path: rootfsDirA},
		},
		Command: []string{"/bin/sh"},
		Cwd:     "/workspace-a",
	}
	lrt, first, err := fixture.controller.ensureLangRuntime(t.Context(), firstRuntime)
	if err != nil {
		t.Fatalf("ensureLangRuntime(first) error = %v", err)
	}
	lrt.IncRef()
	lrt.DecRef()

	driftedRuntime := &runtimeapi.RuntimeTemplate{
		ID:      "start-metrics-drift-runtime",
		Sandbox: "runsc",
		Rootfs: &runtimeapi.RootfsConfig{
			Type:   runtimeapi.RootfsSrcType_LOCAL,
			Source: &runtimeapi.RootfsConfig_Path{Path: rootfsDirB},
		},
		Command: []string{"/bin/bash"},
		Cwd:     "/workspace-b",
	}
	replaced, second, err := fixture.controller.ensureLangRuntime(t.Context(), driftedRuntime)
	if err != nil {
		t.Fatalf("ensureLangRuntime(drifted) error = %v", err)
	}
	if replaced == lrt {
		t.Fatal("expected drifted spec to replace retained runtime")
	}
	if first.RuntimeReused {
		t.Fatalf("first ensureLangRuntime() reused = true, want false")
	}
	if second.RuntimeReused {
		t.Fatalf("drifted ensureLangRuntime() reused = true, want false")
	}
	if second.StartClass() != contract.StartupClassCold {
		t.Fatalf("drifted start class = %q, want %q", second.StartClass(), contract.StartupClassCold)
	}
}

func TestRootfsTypeFromRuntimeTemplate(t *testing.T) {
	tests := []struct {
		name string
		fr   *runtimeapi.RuntimeTemplate
		want string
	}{
		{
			name: "local",
			fr: &runtimeapi.RuntimeTemplate{
				Rootfs: &runtimeapi.RootfsConfig{
					Type:   runtimeapi.RootfsSrcType_LOCAL,
					Source: &runtimeapi.RootfsConfig_Path{Path: "/tmp/rootfs"},
				},
			},
			want: contract.StartupRootfsTypeLocal,
		},
		{
			name: "image",
			fr: &runtimeapi.RuntimeTemplate{
				Rootfs: &runtimeapi.RootfsConfig{
					Type:   runtimeapi.RootfsSrcType_IMAGE,
					Source: &runtimeapi.RootfsConfig_ImageUrl{ImageUrl: "docker.io/library/alpine:latest"},
				},
			},
			want: contract.StartupRootfsTypeImage,
		},
		{
			name: "s3",
			fr: &runtimeapi.RuntimeTemplate{
				Rootfs: &runtimeapi.RootfsConfig{
					Type: runtimeapi.RootfsSrcType_S3,
					Source: &runtimeapi.RootfsConfig_S3Config{
						S3Config: &runtimeapi.S3Config{Endpoint: "oss.example.com", Bucket: "bucket", Object: "rootfs.raw"},
					},
				},
			},
			want: contract.StartupRootfsTypeS3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RootfsTypeFromRuntimeTemplate(tt.fr); got != tt.want {
				t.Fatalf("RootfsTypeFromRuntimeTemplate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func assertStartupSteps(t *testing.T, samples []StartupStepSample, steps ...contract.StartupStep) {
	t.Helper()
	seen := make(map[contract.StartupStep]bool, len(samples))
	for _, sample := range samples {
		if sample.Duration <= 0 {
			t.Fatalf("step %q duration = %v, want > 0", sample.Step, sample.Duration)
		}
		seen[sample.Step] = true
	}
	for _, step := range steps {
		if !seen[step] {
			t.Fatalf("startup steps = %#v, missing %q", samples, step)
		}
	}
}

func TestStartMetricsRecorderSuccess(t *testing.T) {
	sink := &fakeStartMetricSink{}
	recorder := NewStartMetricsRecorder(sink, "runsc", contract.StartupRootfsTypeLocal)

	recorder.SetStartClass(contract.StartupClassWarm)
	recorder.RecordStartupPhase(contract.StartupPhaseLangRuntimeLookup, 5*time.Millisecond)
	recorder.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, 25*time.Millisecond)
	recorder.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepSandboxdWaitReady, 7*time.Millisecond)
	recorder.Finish(contract.StartupResultOK)

	if len(sink.results) != 1 {
		t.Fatalf("result samples = %d, want 1", len(sink.results))
	}
	if sink.results[0].Result != contract.StartupResultOK {
		t.Fatalf("result label = %q, want %q", sink.results[0].Result, contract.StartupResultOK)
	}
	if sink.results[0].StartClass != contract.StartupClassWarm {
		t.Fatalf("start class = %q, want %q", sink.results[0].StartClass, contract.StartupClassWarm)
	}
	if len(sink.phases) != 2 {
		t.Fatalf("phase samples = %d, want 2", len(sink.phases))
	}
	if len(sink.steps) != 1 {
		t.Fatalf("step samples = %d, want 1", len(sink.steps))
	}
	if sink.steps[0].Step != contract.StartupStepSandboxdWaitReady {
		t.Fatalf("step = %q, want %q", sink.steps[0].Step, contract.StartupStepSandboxdWaitReady)
	}
	if len(sink.starts) != 1 {
		t.Fatalf("start duration samples = %d, want 1", len(sink.starts))
	}
	if sink.starts[0].Duration <= 0 {
		t.Fatalf("start duration = %v, want > 0", sink.starts[0].Duration)
	}
}

func TestStartMetricsRecorderError(t *testing.T) {
	sink := &fakeStartMetricSink{}
	recorder := NewStartMetricsRecorder(sink, "runc", contract.StartupRootfsTypeImage)

	recorder.RecordStartupPhase(contract.StartupPhaseLangRuntimeLookup, 3*time.Millisecond)
	recorder.RecordStartupPhase(contract.StartupPhaseRootfsPrepare, 17*time.Millisecond)
	recorder.Finish(contract.StartupResultError)

	if len(sink.results) != 1 {
		t.Fatalf("result samples = %d, want 1", len(sink.results))
	}
	if sink.results[0].Result != contract.StartupResultError {
		t.Fatalf("result label = %q, want %q", sink.results[0].Result, contract.StartupResultError)
	}
	if sink.results[0].RootfsType != contract.StartupRootfsTypeImage {
		t.Fatalf("rootfs type = %q, want %q", sink.results[0].RootfsType, contract.StartupRootfsTypeImage)
	}
	if len(sink.phases) != 2 {
		t.Fatalf("phase samples = %d, want 2", len(sink.phases))
	}
	for _, sample := range sink.phases {
		if sample.Result != contract.StartupResultError {
			t.Fatalf("phase result = %q, want %q", sample.Result, contract.StartupResultError)
		}
	}
}

func TestStartMetricsRecorderDeferredResultUsesFinalValue(t *testing.T) {
	sink := &fakeStartMetricSink{}
	recorder := NewStartMetricsRecorder(sink, "runsc", contract.StartupRootfsTypeLocal)
	result := contract.StartupResultError

	func() {
		defer func() {
			recorder.Finish(result)
		}()
		result = contract.StartupResultOK
	}()

	if len(sink.results) != 1 {
		t.Fatalf("result samples = %d, want 1", len(sink.results))
	}
	if sink.results[0].Result != contract.StartupResultOK {
		t.Fatalf("result label = %q, want %q", sink.results[0].Result, contract.StartupResultOK)
	}
}

func TestStartManagedContainerRecordsSuccessResult(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("requires Linux cgroup support")
	}

	handler := &runtimeSpyHandler{
		name:           "runsc",
		bundleDuration: 4 * time.Millisecond,
		launchDuration: 9 * time.Millisecond,
	}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{
		"runsc": handler,
	})
	sink := &fakeStartMetricSink{}
	fixture.controller.SetStartMetricSink(sink)

	rootfsDir := t.TempDir()
	request := &runtimeapi.StartRequest{
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID:      "start-metrics-managed-success",
			Sandbox: "runsc",
			Rootfs: &runtimeapi.RootfsConfig{
				Type:   runtimeapi.RootfsSrcType_LOCAL,
				Source: &runtimeapi.RootfsConfig_Path{Path: rootfsDir},
			},
			Command: []string{"/bin/true"},
			Cwd:     "/",
		},
		Stdout: filepath.Join(t.TempDir(), "stdout.log"),
		Stderr: filepath.Join(t.TempDir(), "stderr.log"),
	}

	resp, err := fixture.controller.startManagedContainer(context.Background(), request)
	if err != nil {
		t.Fatalf("startManagedContainer() error = %v", err)
	}
	if resp.GetCode() != 0 || resp.GetID() == "" {
		t.Fatalf("startManagedContainer() response = %+v, want successful container id", resp)
	}
	if len(sink.results) != 1 {
		t.Fatalf("result samples = %d, want 1", len(sink.results))
	}
	if sink.results[0].Result != contract.StartupResultOK {
		t.Fatalf("result label = %q, want %q", sink.results[0].Result, contract.StartupResultOK)
	}
	if sink.results[0].StartClass != contract.StartupClassCold {
		t.Fatalf("start class = %q, want %q", sink.results[0].StartClass, contract.StartupClassCold)
	}
	if len(sink.steps) == 0 {
		t.Fatal("expected startup step samples")
	}
	managedSteps := make([]StartupStepSample, 0, len(sink.steps))
	for _, sample := range sink.steps {
		managedSteps = append(managedSteps, StartupStepSample{Phase: sample.Phase, Step: sample.Step, Duration: sample.Duration})
	}
	assertStartupSteps(t, managedSteps, contract.StartupStepRootfsResolve, contract.StartupStepRootfsMount)
}
