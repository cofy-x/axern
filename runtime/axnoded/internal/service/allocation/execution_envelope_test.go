package allocation

import (
	"context"
	goruntime "runtime"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type executionEnvelopeSpyHandler struct {
	runtimeName   string
	createCalls   int
	deleteCalls   int
	prepareCalls  int
	activateCalls int
}

func (h *executionEnvelopeSpyHandler) Name() string {
	if h.runtimeName == "" {
		return "runsc"
	}
	return h.runtimeName
}

func (h *executionEnvelopeSpyHandler) Capabilities() contract.RuntimeCapabilities {
	return contract.RuntimeCapabilities{CanExecDirect: true}
}

func (h *executionEnvelopeSpyHandler) Requirements() contract.RuntimeRequirements {
	return contract.RuntimeRequirements{}
}

func (h *executionEnvelopeSpyHandler) Version(context.Context) (*runtimeapi.RuntimeVersion, error) {
	return &runtimeapi.RuntimeVersion{RuntimeName: h.Name(), RuntimeVersion: "test"}, nil
}

func (h *executionEnvelopeSpyHandler) CreateContainer(_ context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	h.createCalls++
	return &apipb.ContainerMetadata{
		ID:             options.ContainerID,
		RuntimeHandler: h.Name(),
		Labels:         request.Labels,
		Stdout:         request.Stdout,
		Stderr:         request.Stderr,
	}, nil
}

func (h *executionEnvelopeSpyHandler) DeleteContainer(_ context.Context, _ *apipb.DeleteContainerRequest, _ contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
	h.deleteCalls++
	return &apipb.DeleteContainerResponse{}, nil
}

func (h *executionEnvelopeSpyHandler) KillContainer(_ context.Context, _ *apipb.SignalContainerRequest, _ contract.HandlerOptions) (*apipb.SignalContainerResponse, error) {
	return &apipb.SignalContainerResponse{}, nil
}

func (h *executionEnvelopeSpyHandler) ListContainers(_ context.Context, _ contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return []*contract.UnionContainerState{}, nil
}

func (h *executionEnvelopeSpyHandler) ContainerSpec(_ context.Context, _ contract.HandlerOptions) (*specs.Spec, error) {
	return &specs.Spec{}, nil
}

func (h *executionEnvelopeSpyHandler) ExecContainer(_ context.Context, _ *apipb.ExecContainerRequest, _ contract.HandlerOptions) (*apipb.ExecContainerResponse, error) {
	return &apipb.ExecContainerResponse{}, nil
}

func (h *executionEnvelopeSpyHandler) OpenExecSession(_ context.Context, _ *apipb.ExecSessionOpen, _ contract.HandlerOptions) (contract.Session, error) {
	return nil, nil
}

func (h *executionEnvelopeSpyHandler) ProcessService() contract.ProcessService {
	return nil
}

func (h *executionEnvelopeSpyHandler) FileService() contract.FileService {
	return nil
}

func (h *executionEnvelopeSpyHandler) CheckpointContainer(*runtimeapi.CheckpointRequest) error {
	return nil
}

func (h *executionEnvelopeSpyHandler) Wait(_ context.Context, _ contract.HandlerOptions) (contract.Exit, error) {
	return contract.Exit{}, nil
}

func (h *executionEnvelopeSpyHandler) ShutDown() {}

func (h *executionEnvelopeSpyHandler) EligibleForExecutionEnvelope(request *runtimeapi.StartRequest) bool {
	if request == nil || request.RuntimeTemplate == nil {
		return false
	}
	if request.RuntimeTemplate.Sandbox != h.Name() {
		return false
	}
	if request.CkptDir != "" || request.Network != "" || request.Stdout != "" || request.Stderr != "" {
		return false
	}
	if len(request.UserEnvs) > 0 || len(request.Mounts) > 0 || request.GetResources() != nil {
		return false
	}
	return request.ExtraConfig == ""
}

func (h *executionEnvelopeSpyHandler) PrepareExecutionEnvelope(_ context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*contract.ExecutionEnvelope, error) {
	h.prepareCalls++
	return &contract.ExecutionEnvelope{
		ContainerID: options.ContainerID,
		Metadata: &apipb.ContainerMetadata{
			ID:             options.ContainerID,
			RuntimeHandler: h.Name(),
			Labels:         request.Labels,
			Stdout:         request.Stdout,
			Stderr:         request.Stderr,
		},
	}, nil
}

func (h *executionEnvelopeSpyHandler) ActivateExecutionEnvelope(_ context.Context, envelope *contract.ExecutionEnvelope, _ contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	h.activateCalls++
	return envelope.Metadata, nil
}

func TestStartManagedContainerUsesPreparedExecutionEnvelope(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("requires Linux cgroup support")
	}
	handler := &executionEnvelopeSpyHandler{}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	fixture.lrtManager.ConfigureRetention(time.Minute, 8)

	request := &runtimeapi.StartRequest{
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID:      "exec-envelope-hit",
			Sandbox: "runsc",
			Rootfs: &runtimeapi.RootfsConfig{
				Type:   runtimeapi.RootfsSrcType_LOCAL,
				Source: &runtimeapi.RootfsConfig_Path{Path: t.TempDir()},
			},
			Command: []string{"/bin/sh", "-c", "sleep 1"},
			Cwd:     "/",
		},
	}

	lrt, _, err := fixture.controller.ensureLangRuntime(t.Context(), request.RuntimeTemplate)
	if err != nil {
		t.Fatalf("ensureLangRuntime() error = %v", err)
	}
	lrt.IncRef()
	lrt.DecRef()
	fixture.controller.scheduleExecutionEnvelopePrepare(lrt)
	waitForEnvelopeReady(t, lrt)

	resp, err := fixture.controller.startManagedContainer(context.Background(), request)
	if err != nil {
		t.Fatalf("startManagedContainer() error = %v", err)
	}
	if resp.GetCode() != 0 || resp.GetID() == "" {
		t.Fatalf("startManagedContainer() response = %+v, want success", resp)
	}
	if handler.activateCalls != 1 {
		t.Fatalf("activateCalls = %d, want 1", handler.activateCalls)
	}
	if handler.createCalls != 0 {
		t.Fatalf("createCalls = %d, want 0 on envelope hit", handler.createCalls)
	}
}

func TestScheduleExecutionEnvelopePrepareSkipsDisabledRuntime(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("requires Linux cgroup support")
	}
	handler := &executionEnvelopeSpyHandler{}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	fixture.lrtManager.ConfigureRetention(time.Minute, 8)

	template := &runtimeapi.RuntimeTemplate{
		ID:      "exec-envelope-disabled",
		Sandbox: "runsc",
		Rootfs: &runtimeapi.RootfsConfig{
			Type:   runtimeapi.RootfsSrcType_LOCAL,
			Source: &runtimeapi.RootfsConfig_Path{Path: t.TempDir()},
		},
		Command: []string{"/bin/sh", "-c", "sleep 1"},
		Cwd:     "/",
	}

	lrt, _, err := fixture.controller.ensureLangRuntime(t.Context(), template)
	if err != nil {
		t.Fatalf("ensureLangRuntime() error = %v", err)
	}
	lrt.SetExecutionEnvelopeEnabled(false)
	lrt.IncRef()
	lrt.DecRef()
	fixture.controller.scheduleExecutionEnvelopePrepare(lrt)

	time.Sleep(50 * time.Millisecond)
	if handler.prepareCalls != 0 {
		t.Fatalf("prepareCalls = %d, want 0 for disabled runtime", handler.prepareCalls)
	}
	if lrt.HasReadyExecutionEnvelope() {
		t.Fatal("disabled runtime should not prepare an execution envelope")
	}
}

func TestStartManagedContainerUsesPreparedExecutionEnvelopeForRunc(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("requires Linux cgroup support")
	}
	handler := &executionEnvelopeSpyHandler{runtimeName: "runc"}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runc": handler})
	fixture.lrtManager.ConfigureRetention(time.Minute, 8)

	request := &runtimeapi.StartRequest{
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID:      "exec-envelope-hit-runc",
			Sandbox: "runc",
			Rootfs: &runtimeapi.RootfsConfig{
				Type:   runtimeapi.RootfsSrcType_LOCAL,
				Source: &runtimeapi.RootfsConfig_Path{Path: t.TempDir()},
			},
			Command: []string{"/bin/sh", "-c", "sleep 1"},
			Cwd:     "/",
		},
	}

	lrt, _, err := fixture.controller.ensureLangRuntime(t.Context(), request.RuntimeTemplate)
	if err != nil {
		t.Fatalf("ensureLangRuntime() error = %v", err)
	}
	lrt.IncRef()
	lrt.DecRef()
	fixture.controller.scheduleExecutionEnvelopePrepare(lrt)
	waitForEnvelopeReady(t, lrt)

	resp, err := fixture.controller.startManagedContainer(context.Background(), request)
	if err != nil {
		t.Fatalf("startManagedContainer() error = %v", err)
	}
	if resp.GetCode() != 0 || resp.GetID() == "" {
		t.Fatalf("startManagedContainer() response = %+v, want success", resp)
	}
	if handler.activateCalls != 1 {
		t.Fatalf("activateCalls = %d, want 1", handler.activateCalls)
	}
	if handler.createCalls != 0 {
		t.Fatalf("createCalls = %d, want 0 on envelope hit", handler.createCalls)
	}
}

func TestStartManagedContainerDynamicRequestFallsBackAndClearsEnvelope(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("requires Linux cgroup support")
	}
	handler := &executionEnvelopeSpyHandler{}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	fixture.lrtManager.ConfigureRetention(time.Minute, 8)

	request := &runtimeapi.StartRequest{
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID:      "exec-envelope-fallback",
			Sandbox: "runsc",
			Rootfs: &runtimeapi.RootfsConfig{
				Type:   runtimeapi.RootfsSrcType_LOCAL,
				Source: &runtimeapi.RootfsConfig_Path{Path: t.TempDir()},
			},
			Command: []string{"/bin/sh", "-c", "sleep 1"},
			Cwd:     "/",
		},
		UserEnvs: map[string]string{"FOO": "bar"},
	}

	lrt, _, err := fixture.controller.ensureLangRuntime(t.Context(), request.RuntimeTemplate)
	if err != nil {
		t.Fatalf("ensureLangRuntime() error = %v", err)
	}
	lrt.IncRef()
	lrt.DecRef()
	fixture.controller.scheduleExecutionEnvelopePrepare(lrt)
	waitForEnvelopeReady(t, lrt)

	resp, err := fixture.controller.startManagedContainer(context.Background(), request)
	if err != nil {
		t.Fatalf("startManagedContainer() error = %v", err)
	}
	if resp.GetCode() != 0 || resp.GetID() == "" {
		t.Fatalf("startManagedContainer() response = %+v, want success", resp)
	}
	if handler.activateCalls != 0 {
		t.Fatalf("activateCalls = %d, want 0 for fallback request", handler.activateCalls)
	}
	if handler.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1 for fallback request", handler.createCalls)
	}
	if lrt.HasReadyExecutionEnvelope() {
		t.Fatal("expected ready execution envelope to be cleared on dynamic fallback")
	}
	if lrt.ExecutionEnvelopeEnabled() {
		t.Fatal("dynamic request should disable future execution envelope preparation")
	}

	request.UserEnvs = nil
	request.ContainerID = "static-after-dynamic"
	resp, err = fixture.controller.startManagedContainer(context.Background(), request)
	if err != nil || resp.GetCode() != 0 {
		t.Fatalf("static start after dynamic fallback = (%+v, %v), want success", resp, err)
	}
	if !lrt.ExecutionEnvelopeEnabled() {
		t.Fatal("eligible request should re-enable future execution envelope preparation")
	}
}

func TestStartManagedContainerDynamicRuncRequestFallsBackAndClearsEnvelope(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("requires Linux cgroup support")
	}
	handler := &executionEnvelopeSpyHandler{runtimeName: "runc"}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runc": handler})
	fixture.lrtManager.ConfigureRetention(time.Minute, 8)

	request := &runtimeapi.StartRequest{
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID:      "exec-envelope-fallback-runc",
			Sandbox: "runc",
			Rootfs: &runtimeapi.RootfsConfig{
				Type:   runtimeapi.RootfsSrcType_LOCAL,
				Source: &runtimeapi.RootfsConfig_Path{Path: t.TempDir()},
			},
			Command: []string{"/bin/sh", "-c", "sleep 1"},
			Cwd:     "/",
		},
		UserEnvs: map[string]string{"FOO": "bar"},
	}

	lrt, _, err := fixture.controller.ensureLangRuntime(t.Context(), request.RuntimeTemplate)
	if err != nil {
		t.Fatalf("ensureLangRuntime() error = %v", err)
	}
	lrt.IncRef()
	lrt.DecRef()
	fixture.controller.scheduleExecutionEnvelopePrepare(lrt)
	waitForEnvelopeReady(t, lrt)

	resp, err := fixture.controller.startManagedContainer(context.Background(), request)
	if err != nil {
		t.Fatalf("startManagedContainer() error = %v", err)
	}
	if resp.GetCode() != 0 || resp.GetID() == "" {
		t.Fatalf("startManagedContainer() response = %+v, want success", resp)
	}
	if handler.activateCalls != 0 {
		t.Fatalf("activateCalls = %d, want 0 for fallback request", handler.activateCalls)
	}
	if handler.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1 for fallback request", handler.createCalls)
	}
	if lrt.HasReadyExecutionEnvelope() {
		t.Fatal("expected ready execution envelope to be cleared on dynamic fallback")
	}
	if lrt.ExecutionEnvelopeEnabled() {
		t.Fatal("dynamic request should disable future execution envelope preparation")
	}
}

func waitForEnvelopeReady(t *testing.T, lrt *langrtmanager.LanguageRuntime) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if lrt.HasReadyExecutionEnvelope() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for execution envelope to become ready")
}
