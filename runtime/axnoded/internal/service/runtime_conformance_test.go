package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

type loadedConformanceRuntime struct {
	*runtimetest.FakeSandboxRuntime
	loader *oci.BundleLoader
}

func (h *loadedConformanceRuntime) ConfigurationDigest() (string, error) {
	return h.loader.ConfigurationDigest()
}

func TestRuntimeConformanceUsesLoadedPolicyNotFiles(t *testing.T) {
	cfg := runtimeConformanceTestConfig(t, config.CgroupEnforcementRequired)
	root := t.TempDir()
	loader, err := oci.NewBundleLoader(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := &loadedConformanceRuntime{runtimetest.NewFakeSandboxRuntime(), loader}
	calls := 0
	p := runtimeConformanceCapabilityProvider(cfg, handler, runtimeConformanceKindMemory, testCapabilityBootID, func(context.Context, runtimeConformanceKind) error { calls++; return nil })
	before, _, configBefore, err := p.runtimeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := handler.ConfigurationDigest()
	if err != nil {
		t.Fatal(err)
	}
	if configBefore != actual {
		t.Fatal("conformance did not identify actual handler")
	}
	if _, err := p.Observe(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runsc-config.json"), []byte(`{"process":{"env":["TERM=xterm"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	allow := false
	p.cfg.PluginConfig.RuntimeConfig.Runsc.Options.AllowSUID = &allow
	after, _, _, err := p.runtimeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("unloaded config was used to identify existing handler")
	}
	if _, err := p.Observe(t.Context(), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("unloaded configuration triggered destructive recertification")
	}
}

func TestRuntimeConformanceDefersMissingAdmissionCapacityWithoutLatchingFailure(t *testing.T) {
	cfg := runtimeConformanceTestConfig(t, config.CgroupEnforcementRequired)
	calls := 0
	provider := runtimeConformanceCapabilityProvider(cfg, runtimetest.NewFakeSandboxRuntime(), runtimeConformanceKindMemory, testCapabilityBootID, func(context.Context, runtimeConformanceKind) error {
		calls++
		if calls == 1 {
			return resources.ErrMemoryCapacityUnavailable
		}
		return nil
	})
	now := time.Now().UTC()
	for _, offset := range []time.Duration{0, time.Second} {
		observations, err := provider.Observe(t.Context(), now.Add(offset))
		if err != nil || observations[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN || calls != 1 {
			t.Fatalf("capacity-wait observation=%v err=%v calls=%d", observations, err, calls)
		}
	}
	for _, offset := range []time.Duration{10 * time.Second, time.Hour} {
		observations, err := provider.Observe(t.Context(), now.Add(offset))
		if err != nil || observations[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE || calls != 2 {
			t.Fatalf("certified observation=%v err=%v calls=%d", observations, err, calls)
		}
	}
}

func TestRuntimeConformanceProvidersKeepMemoryAndEphemeralIndependent(t *testing.T) {
	cfg := runtimeConformanceTestConfig(t, config.CgroupEnforcementDisabledDev)
	handler := runtimetest.NewFakeSandboxRuntime()

	calls := make(map[runtimeConformanceKind]int)
	probe := func(_ context.Context, kind runtimeConformanceKind) error {
		calls[kind]++
		return nil
	}
	now := time.Now().UTC()
	memory := runtimeConformanceCapabilityProvider(cfg, handler, runtimeConformanceKindMemory, testCapabilityBootID, probe)
	memoryObservations, err := memory.Observe(context.Background(), now)
	if err != nil {
		t.Fatalf("memory Observe() error = %v", err)
	}
	if got := memoryObservations[0].GetState(); got != capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE {
		t.Fatalf("memory state = %s, want unavailable", got)
	}
	if got := memoryObservations[0].GetReasonCode(); got != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DISABLED {
		t.Fatalf("memory reason = %s, want disabled", got)
	}
	if calls[runtimeConformanceKindMemory] != 0 {
		t.Fatalf("disabled memory probe calls = %d, want 0", calls[runtimeConformanceKindMemory])
	}

	ephemeral := runtimeConformanceCapabilityProvider(cfg, handler, runtimeConformanceKindEphemeral, testCapabilityBootID, probe)
	ephemeralObservations, err := ephemeral.Observe(context.Background(), now)
	if err != nil {
		t.Fatalf("ephemeral Observe() error = %v", err)
	}
	if got := ephemeralObservations[0].GetState(); got != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		t.Fatalf("ephemeral state = %s, want available", got)
	}
	if calls[runtimeConformanceKindEphemeral] != 1 {
		t.Fatalf("ephemeral probe calls = %d, want 1", calls[runtimeConformanceKindEphemeral])
	}
}

func TestRuntimeConformanceIdentityChangeInvalidatesBeforeExpensiveReprobe(t *testing.T) {
	cfg := runtimeConformanceTestConfig(t, config.CgroupEnforcementRequired)
	handler := runtimetest.NewFakeSandboxRuntime()
	probeCalls := 0
	provider := runtimeConformanceCapabilityProvider(cfg, handler, runtimeConformanceKindMemory, testCapabilityBootID, func(context.Context, runtimeConformanceKind) error {
		probeCalls++
		return nil
	})
	now := time.Now().UTC()
	initial, err := provider.Observe(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if initial[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE || probeCalls != 1 {
		t.Fatalf("initial observation=%s probes=%d", initial[0].GetState(), probeCalls)
	}

	runtimePath := cfg.PluginConfig.RuntimeConfig.Runsc.Binary
	if err := os.WriteFile(runtimePath, []byte("changed-runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err := provider.Observe(context.Background(), now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if changed[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN || changed[0].GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_IDENTITY_CHANGED {
		t.Fatalf("identity-change observation = state %s reason %s", changed[0].GetState(), changed[0].GetReasonCode())
	}
	if probeCalls != 1 {
		t.Fatalf("identity invalidation blocked on reprobe; calls=%d", probeCalls)
	}

	reprobed, err := provider.Observe(context.Background(), now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if reprobed[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE || probeCalls != 2 {
		t.Fatalf("reprobe observation=%s probes=%d", reprobed[0].GetState(), probeCalls)
	}
}

func TestRuntimeConformanceObservationUsesProbeCompletionTime(t *testing.T) {
	cfg := runtimeConformanceTestConfig(t, config.CgroupEnforcementRequired)
	handler := runtimetest.NewFakeSandboxRuntime()
	provider := runtimeConformanceCapabilityProvider(cfg, handler, runtimeConformanceKindMemory, testCapabilityBootID, func(context.Context, runtimeConformanceKind) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	sampledAt := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	observations, err := provider.Observe(context.Background(), sampledAt)
	if err != nil {
		t.Fatal(err)
	}
	if !observations[0].GetObservedAt().AsTime().After(sampledAt) {
		t.Fatalf("observed_at = %s, want completion after sample start %s", observations[0].GetObservedAt().AsTime(), sampledAt)
	}
}

func TestRuntimeConformanceDoesNotPeriodicallyRepeatDestructiveProbe(t *testing.T) {
	cfg := runtimeConformanceTestConfig(t, config.CgroupEnforcementRequired)
	handler := runtimetest.NewFakeSandboxRuntime()
	probeCalls := 0
	provider := runtimeConformanceCapabilityProvider(cfg, handler, runtimeConformanceKindMemory, testCapabilityBootID, func(context.Context, runtimeConformanceKind) error {
		probeCalls++
		return nil
	})
	now := time.Now().UTC()
	for _, sample := range []time.Time{now, now.Add(15 * time.Minute), now.Add(24 * time.Hour)} {
		observations, err := provider.Observe(context.Background(), sample)
		if err != nil {
			t.Fatal(err)
		}
		if observations[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
			t.Fatalf("observation at %s = %s", sample, observations[0].GetState())
		}
	}
	if probeCalls != 1 {
		t.Fatalf("destructive probe calls = %d, want startup certification only", probeCalls)
	}
}

func TestFailedRuntimeConformanceRemainsLatchedUntilRestart(t *testing.T) {
	cfg := runtimeConformanceTestConfig(t, config.CgroupEnforcementRequired)
	handler := runtimetest.NewFakeSandboxRuntime()
	calls := 0
	probe := func(context.Context, runtimeConformanceKind) error {
		calls++
		return errors.New("certification failed")
	}
	p := runtimeConformanceCapabilityProvider(cfg, handler, runtimeConformanceKindMemory, testCapabilityBootID, probe)
	now := time.Now()
	for _, delay := range []time.Duration{0, time.Minute, time.Hour, 24 * time.Hour} {
		observations, err := p.Observe(context.Background(), now.Add(delay))
		if err != nil || observations[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE {
			t.Fatalf("failed certification not retained: %v %v", observations, err)
		}
	}
	if calls != 1 {
		t.Fatalf("destructive calls = %d, want 1", calls)
	}
	p = runtimeConformanceCapabilityProvider(cfg, handler, runtimeConformanceKindMemory, testCapabilityBootID, probe)
	_, _ = p.Observe(context.Background(), now)
	if calls != 2 {
		t.Fatalf("restart did not recertify: %d", calls)
	}
}

func TestRuntimeConformanceDockerGateMatchesAggregateLimit(t *testing.T) {
	data, err := os.ReadFile("../../scripts/verify/verify-in-container.sh")
	if err != nil {
		t.Fatal(err)
	}
	limit := strconv.FormatInt(config.RuntimeConformanceMemoryMaxBytes, 10)
	for _, assertion := range []string{
		`[ "$(cat "${conformance_dir}/memory.max")" = "` + limit + `" ]`,
		`.node.memory_budget.conformance_limit_bytes == ` + limit + ` and`,
	} {
		if !strings.Contains(string(data), assertion) {
			t.Fatalf("Docker certification assertion does not match aggregate limit: %s", assertion)
		}
	}
}

func TestRuntimeConformanceStartRequestsIsolateEnforcementBoundaries(t *testing.T) {
	if runtimeConformanceMemoryLimit != config.RuntimeConformanceMemoryLimitBytes {
		t.Fatalf("memory self-test limit = %d, want %d", runtimeConformanceMemoryLimit, config.RuntimeConformanceMemoryLimitBytes)
	}
	if config.RuntimeConformanceMemoryMaxBytes <= runtimeConformanceMemoryLimit {
		t.Fatalf("aggregate conformance ceiling %d must exceed workload limit %d", config.RuntimeConformanceMemoryMaxBytes, runtimeConformanceMemoryLimit)
	}
	memory, err := runtimeConformanceStartRequest("memory-allocation", "memory-runtime", "/rootfs", runtimeConformanceKindMemory)
	if err != nil {
		t.Fatalf("memory request error = %v", err)
	}
	if !memory.GetEnvironment().GetRootfs().GetReadonly() {
		t.Fatal("memory self-test rootfs must be readonly")
	}
	if memory.GetResources().GetLimits().GetMemoryBytes() != runtimeConformanceMemoryLimit || memory.GetResources().GetLimits().GetEphemeralStorageBytes() != 0 {
		t.Fatalf("memory limits = %+v", memory.GetResources().GetLimits())
	}

	ephemeral, err := runtimeConformanceStartRequest("storage-allocation", "storage-runtime", "/rootfs", runtimeConformanceKindEphemeral)
	if err != nil {
		t.Fatalf("ephemeral request error = %v", err)
	}
	if ephemeral.GetEnvironment().GetRootfs().GetReadonly() {
		t.Fatal("ephemeral self-test rootfs must be writable")
	}
	if ephemeral.GetResources().GetLimits().GetMemoryBytes() != 0 || ephemeral.GetResources().GetLimits().GetEphemeralStorageBytes() != runtimeConformanceStorage {
		t.Fatalf("ephemeral limits = %+v", ephemeral.GetResources().GetLimits())
	}
}

func runtimeConformanceTestConfig(t *testing.T, cgroupMode string) config.Config {
	t.Helper()
	directory := t.TempDir()
	binary := filepath.Join(directory, "runtime")
	for path, payload := range map[string]string{binary: "runtime"} {
		if err := os.WriteFile(path, []byte(payload), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return config.Config{PluginConfig: config.PluginConfig{RuntimeConfig: config.RuntimeConfig{
		CgroupEnforcement: cgroupMode,
		Runsc:             config.RuntimeInstanceConfig{Binary: binary},
	}}}
}

func TestMaterializeRuntimeConformanceRootfs(t *testing.T) {
	filestore := t.TempDir()
	fixture := t.TempDir()
	fixtureBin := filepath.Join(fixture, "bin")
	if err := os.Mkdir(fixtureBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureBin, "busybox"), []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureBin, "memory-hog"), []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}

	rootfs, err := materializeRuntimeConformanceRootfs(filestore, fixture)
	if err != nil {
		t.Fatalf("materializeRuntimeConformanceRootfs() error = %v", err)
	}
	if err := validateRuntimeConformanceRootfs(rootfs); err != nil {
		t.Fatalf("validateRuntimeConformanceRootfs() error = %v", err)
	}
	second, err := materializeRuntimeConformanceRootfs(filestore, fixture)
	if err != nil {
		t.Fatalf("second materializeRuntimeConformanceRootfs() error = %v", err)
	}
	if second != rootfs {
		t.Fatalf("second rootfs = %q, want %q", second, rootfs)
	}
}

func TestMaterializeRuntimeConformanceRootfsAtomicallyRepairsCorruptPublishedFixture(t *testing.T) {
	filestore := t.TempDir()
	rootfs := filepath.Join(filestore, "system", "runtime-conformance", "rootfs")
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	fixtureBin := filepath.Join(fixture, "bin")
	if err := os.Mkdir(fixtureBin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"busybox", "memory-hog"} {
		if err := os.WriteFile(filepath.Join(fixtureBin, name), []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := materializeRuntimeConformanceRootfs(filestore, fixture)
	if err != nil {
		t.Fatalf("materializeRuntimeConformanceRootfs() error = %v", err)
	}
	if got != rootfs {
		t.Fatalf("rootfs = %q, want %q", got, rootfs)
	}
	if err := validateRuntimeConformanceRootfs(got); err != nil {
		t.Fatalf("validate repaired rootfs: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(rootfs))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".rootfs-") {
			t.Fatalf("stale materialization artifact remains: %s", entry.Name())
		}
	}
}

func TestVerifyRuntimeConformanceCleanupRejectsRemainingArtifact(t *testing.T) {
	root := t.TempDir()
	filestore := t.TempDir()
	service := &sandboxService{config: config.Config{
		RootDir: root,
		PluginConfig: config.PluginConfig{RuntimeConfig: config.RuntimeConfig{
			FilestoreDir:      filestore,
			CgroupEnforcement: config.CgroupEnforcementDisabledDev,
		}},
	}}
	id := "capability-selftest-runsc-memory-allocation"
	if err := service.verifyRuntimeConformanceCleanup(context.Background(), id); err != nil {
		t.Fatalf("empty cleanup verification error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(filestore, "projections", id), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := service.verifyRuntimeConformanceCleanup(ctx, id); err == nil {
		t.Fatal("cleanup verification accepted a remaining projection")
	}
}

func TestVerifyRuntimeConformanceCleanupRequiresManagerForRequiredCgroups(t *testing.T) {
	service := &sandboxService{config: config.Config{
		RootDir: t.TempDir(),
		PluginConfig: config.PluginConfig{RuntimeConfig: config.RuntimeConfig{
			FilestoreDir:      t.TempDir(),
			CgroupEnforcement: config.CgroupEnforcementRequired,
		}},
	}}
	if err := service.verifyRuntimeConformanceCleanup(context.Background(), "self-test"); err == nil {
		t.Fatal("required cgroup cleanup accepted a missing container manager")
	}
}

func TestRuntimeConformanceOperationContextReservesCleanupDeadline(t *testing.T) {
	parentDeadline := time.Now().Add(runtimeConformanceCleanup + time.Second)
	parent, cancelParent := context.WithDeadline(context.Background(), parentDeadline)
	defer cancelParent()

	operation, cancelOperation, err := runtimeConformanceOperationContext(parent)
	if err != nil {
		t.Fatalf("runtimeConformanceOperationContext() error = %v", err)
	}
	defer cancelOperation()
	deadline, ok := operation.Deadline()
	if !ok {
		t.Fatal("operation context has no deadline")
	}
	if delta := parentDeadline.Sub(deadline); delta < runtimeConformanceCleanup-time.Millisecond || delta > runtimeConformanceCleanup+time.Millisecond {
		t.Fatalf("reserved cleanup window = %v, want %v", delta, runtimeConformanceCleanup)
	}
}
