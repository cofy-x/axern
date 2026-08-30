package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	runtimeConformanceFixture = "/opt/axern/runtime-selftest/rootfs"
	runtimeConformanceResult  = "/.axern-quota-result"
	runtimeConformanceTimeout = 60 * time.Second
	runtimeConformanceCleanup = 30 * time.Second
	// Memory and ephemeral storage use separate sandboxes so one unavailable
	// enforcement boundary cannot suppress evidence for the other.
	runtimeConformanceMemoryLimit = config.RuntimeConformanceMemoryMaxBytes
	runtimeConformanceStorage     = 64 << 20
)

var runtimeConformanceRootfsMu sync.Mutex

type runtimeConformanceKind string

const (
	runtimeConformanceKindMemory    runtimeConformanceKind = "memory"
	runtimeConformanceKindEphemeral runtimeConformanceKind = "ephemeral-storage"
)

type runtimeConformanceProbe func(context.Context, string, runtimeConformanceKind) error

type runtimeConformanceProvider struct {
	mu               sync.Mutex
	cfg              config.Config
	registry         *handlerregistry.Registry
	runtime          string
	kind             runtimeConformanceKind
	provider         capabilityv1.CapabilityProvider
	expected         *capabilityv1.CapabilityKey
	bootID           string
	probe            runtimeConformanceProbe
	identity         string
	lastProbe        time.Time
	nextProbe        time.Time
	failures         int
	lastErr          error
	lastErrorUnknown bool
	lastReasonCode   capabilityv1.CapabilityReasonCode
	digestCache      *runtimeFileDigestCache
}

type runtimeFileDigestEntry struct {
	info   os.FileInfo
	digest string
}

type runtimeFileDigestCache struct {
	mu      sync.Mutex
	entries map[string]runtimeFileDigestEntry
}

func newRuntimeFileDigestCache() *runtimeFileDigestCache {
	return &runtimeFileDigestCache{entries: make(map[string]runtimeFileDigestEntry)}
}

func (c *runtimeFileDigestCache) Digest(path string) (string, error) {
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.entries[path]; ok && runtimeFileMetadataEqual(cached.info, info) {
		return cached.digest, nil
	}
	digest, err := digestFile(path)
	if err != nil {
		return "", err
	}
	c.entries[path] = runtimeFileDigestEntry{info: info, digest: digest}
	return digest, nil
}

func runtimeConformanceCapabilityProvider(cfg config.Config, registry *handlerregistry.Registry, runtimeName string, kind runtimeConformanceKind, bootID string, probe runtimeConformanceProbe, caches ...*runtimeFileDigestCache) *runtimeConformanceProvider {
	// The call sites use the closed runtime/kind matrix below. Keep each
	// provider single-keyed: provider ownership and recovery are tracked per
	// observation, so combining enforcement boundaries would couple their
	// failure and refresh lifecycles again.
	provider := capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST
	fact := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST
	if kind == runtimeConformanceKindEphemeral {
		fact = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST
	}
	if runtimeName == config.RuntimeNameRunsc {
		provider = capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST
		fact = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST
		if kind == runtimeConformanceKindEphemeral {
			fact = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST
		}
	}
	cache := newRuntimeFileDigestCache()
	if len(caches) > 0 && caches[0] != nil {
		cache = caches[0]
	}
	return &runtimeConformanceProvider{
		cfg: cfg, registry: registry, runtime: runtimeName, kind: kind, provider: provider, bootID: bootID, probe: probe,
		expected: capabilitycontract.PlatformKey(fact), digestCache: cache,
	}
}

func (p *runtimeConformanceProvider) Provider() capabilityv1.CapabilityProvider { return p.provider }
func (p *runtimeConformanceProvider) ExpectedKeys() []*capabilityv1.CapabilityKey {
	return []*capabilityv1.CapabilityKey{capabilitycontract.CloneKey(p.expected)}
}

func (p *runtimeConformanceProvider) Observe(ctx context.Context, now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sampleStarted := time.Now()
	identity, binaryDigest, configDigest, err := p.runtimeIdentity()
	identityChanged := err == nil && identity != p.identity
	// Conformance creates a destructive sandbox (real OOM or quota fill). A
	// successful result is bound to the runtime/config identity and is not a
	// health sample: rerun it only for first certification, identity changes,
	// or failure retry. Cheap runtime identity and allocation control audits
	// provide the continuous enforcement signal.
	probeDue := p.lastProbe.IsZero() || (!p.nextProbe.IsZero() && !now.Before(p.nextProbe))
	disabledReason := ""
	if err == nil && p.kind == runtimeConformanceKindMemory {
		mode, modeErr := p.cfg.PluginConfig.RuntimeConfig.CgroupEnforcementMode()
		if modeErr != nil {
			err = modeErr
		} else if mode != config.CgroupEnforcementRequired {
			disabledReason = "cgroup enforcement is disabled for development"
		}
	}
	// A previously certified runtime/config subject must stop satisfying new
	// admission as soon as its identity changes. Publish UNKNOWN first; the
	// next scheduler tick performs the expensive conformance sandbox. Running
	// the self-test inline here would leave the old AVAILABLE batch visible for
	// up to the full probe timeout.
	if err == nil && identityChanged && !p.lastProbe.IsZero() && disabledReason == "" {
		p.identity = identity
		p.lastProbe = runtimeSampleCompletedAt(now, sampleStarted)
		p.lastErr = errors.New("runtime or runtime configuration identity changed; conformance revalidation is pending")
		p.lastErrorUnknown = true
		p.lastReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_IDENTITY_CHANGED
		p.failures = 1
		p.nextProbe = p.lastProbe
	} else if err == nil && disabledReason != "" && (identityChanged || probeDue) {
		p.identity = identity
		p.lastProbe = runtimeSampleCompletedAt(now, sampleStarted)
		p.lastErr = errors.New(disabledReason)
		p.lastErrorUnknown = false
		p.lastReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DISABLED
		p.failures = 0
		p.nextProbe = time.Time{}
	} else if err == nil && (identityChanged || probeDue) {
		probeCtx, cancel := context.WithTimeout(ctx, runtimeConformanceTimeout)
		err = p.probe(probeCtx, p.runtime, p.kind)
		cancel()
		p.identity = identity
		p.lastProbe = runtimeSampleCompletedAt(now, sampleStarted)
		p.lastErr = err
		p.lastErrorUnknown = false
		p.lastReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
		if err != nil {
			p.failures++
			p.nextProbe = p.lastProbe.Add(runtimeProbeRetryDelay(p.failures))
		} else {
			p.failures = 0
			p.nextProbe = time.Time{}
		}
	} else if err != nil && (p.lastProbe.IsZero() || p.nextProbe.IsZero() || !now.Before(p.nextProbe)) {
		p.identity = identity
		p.lastProbe = runtimeSampleCompletedAt(now, sampleStarted)
		p.lastErr = err
		p.lastErrorUnknown = true
		p.lastReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR
		p.failures++
		p.nextProbe = p.lastProbe.Add(runtimeProbeRetryDelay(p.failures))
	}
	var evidence *capabilityv1.CapabilityEvidence
	if binaryDigest != "" && configDigest != "" {
		evidence = capabilitycontract.RuntimeEvidence(p.bootID, p.runtime, binaryDigest, configDigest)
	}
	if p.lastErr != nil {
		observation := failedObservation(p.expected, evidence, p.lastReasonCode, p.lastErr.Error())
		if p.lastErrorUnknown {
			observation.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN
		}
		observations := []*capabilityv1.CapabilityObservation{observation}
		setObservationTime(observations, p.lastProbe)
		return observations, nil
	}
	result := []*capabilityv1.CapabilityObservation{availableObservation(p.expected, evidence)}
	setObservationTime(result, p.lastProbe)
	return result, nil
}

func runtimeSampleCompletedAt(sampledAt, wallStarted time.Time) time.Time {
	elapsed := time.Since(wallStarted)
	if elapsed < 0 {
		elapsed = 0
	}
	return sampledAt.Add(elapsed)
}

func runtimeProbeRetryDelay(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	delay := 5 * time.Second
	for i := 1; i < failures && delay < 5*time.Minute; i++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func setObservationTime(observations []*capabilityv1.CapabilityObservation, observedAt time.Time) {
	for _, observation := range observations {
		observation.ObservedAt = timestamppb.New(observedAt.UTC())
	}
}

func (p *runtimeConformanceProvider) runtimeIdentity() (identity, binaryDigest, configDigest string, err error) {
	runtimeCfg, configured := p.cfg.PluginConfig.RuntimeConfig.NormalizedRuntimeConfigs()[p.runtime]
	if !configured {
		return "", "", "", fmt.Errorf("runtime %q is not configured", p.runtime)
	}
	if _, loaded := p.registry.Get(p.runtime); !loaded {
		return "", "", "", fmt.Errorf("runtime %q handler is not loaded", p.runtime)
	}
	runtimeBinary, err := exec.LookPath(strings.TrimSpace(runtimeCfg.Binary))
	if err != nil {
		return "", "", "", fmt.Errorf("resolve runtime binary: %w", err)
	}
	binaryDigest, err = p.digestCache.Digest(runtimeBinary)
	if err != nil {
		return "", "", "", fmt.Errorf("digest runtime binary: %w", err)
	}
	baseSpecDigest, err := p.digestCache.Digest(runtimeCfg.BaseSpec)
	if err != nil {
		return "", "", "", fmt.Errorf("digest runtime base spec: %w", err)
	}
	runner, err := exec.LookPath(p.cfg.PluginConfig.RuntimeConfig.RuntimeRunnerBinaryPath())
	if err != nil {
		return "", "", "", fmt.Errorf("resolve runtime runner binary: %w", err)
	}
	runnerDigest, err := p.digestCache.Digest(runner)
	if err != nil {
		return "", "", "", fmt.Errorf("digest runtime runner binary: %w", err)
	}
	mode, err := p.cfg.PluginConfig.RuntimeConfig.CgroupEnforcementMode()
	if err != nil {
		return "", "", "", err
	}
	options, err := json.Marshal(runtimeCfg.Options)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal runtime options: %w", err)
	}
	configPayload := strings.Join([]string{baseSpecDigest, runnerDigest, string(options), mode}, "\x00")
	digest := sha256.Sum256([]byte(configPayload))
	configDigest = "sha256:" + hex.EncodeToString(digest[:])
	return binaryDigest + ":" + configDigest, binaryDigest, configDigest, nil
}

func digestFile(path string) (string, error) {
	payload, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (h *sandboxService) runRuntimeConformanceSelfTest(ctx context.Context, runtimeName string, kind runtimeConformanceKind) (retErr error) {
	rootfs, err := materializeRuntimeConformanceRootfs(
		h.config.PluginConfig.RuntimeConfig.FilestoreDir,
		runtimeConformanceFixture,
	)
	if err != nil {
		return err
	}
	runtimeID := "capability-selftest-" + runtimeName + "-" + string(kind)
	// Providers are serialized per runtime/kind. A deterministic allocation ID
	// makes interrupted probes reconcilable and prevents retries from creating
	// an unbounded series of orphan bundles.
	allocationID := runtimeID + "-allocation"
	preflightCtx, preflightCancel := context.WithTimeout(ctx, runtimeConformanceCleanup)
	if err := h.cleanupRuntimeConformanceAllocation(preflightCtx, runtimeName, allocationID); err != nil {
		preflightCancel()
		return fmt.Errorf("cleanup previous runtime conformance sandbox: %w", err)
	}
	preflightCancel()
	request, err := runtimeConformanceStartRequest(allocationID, runtimeID, runtimeName, rootfs, kind)
	if err != nil {
		return err
	}
	operationCtx, operationCancel, err := runtimeConformanceOperationContext(ctx)
	if err != nil {
		return err
	}
	defer operationCancel()
	startAttempted := false
	defer func() {
		if !startAttempted {
			return
		}
		// Cleanup is a compensating action and must survive probe cancellation
		// and service shutdown. The operation deadline reserves this bounded
		// cleanup window inside the provider's 60-second conformance budget.
		deleteCtx, cancel := context.WithTimeout(context.Background(), runtimeConformanceCleanup)
		defer cancel()
		if err := h.cleanupRuntimeConformanceAllocation(deleteCtx, runtimeName, allocationID); err != nil {
			if retErr == nil {
				retErr = fmt.Errorf("cleanup self-test allocation: %w", err)
			} else {
				retErr = fmt.Errorf("%w; cleanup self-test allocation: %v", retErr, err)
			}
			return
		}
		if err := h.lrtManager.EvictIdleRuntime(deleteCtx, runtimeID, langrtmanager.RetentionReasonSelfTest); err != nil {
			if retErr == nil {
				retErr = fmt.Errorf("cleanup self-test runtime: %w", err)
			} else {
				retErr = fmt.Errorf("%w; cleanup self-test runtime: %v", retErr, err)
			}
		}
	}()
	startAttempted = true
	response, err := h.allocationController().StartInternalConformance(operationCtx, request)
	if err != nil || response == nil || response.GetCode() != 0 {
		message := "empty response"
		if response != nil {
			message = response.GetMessage()
		}
		return fmt.Errorf("start runtime conformance sandbox: %w", firstRuntimeConformanceError(err, message))
	}
	if kind == runtimeConformanceKindEphemeral {
		if err := h.verifyRuntimeConformanceQuotaResult(operationCtx, allocationID); err != nil {
			return err
		}
	} else if kind == runtimeConformanceKindMemory {
		return h.verifyRuntimeConformanceMemoryOOM(operationCtx, allocationID)
	}
	platform, err := runtimeConformancePlatform(runtimeName, kind)
	if err != nil {
		return err
	}
	key := capabilitycontract.PlatformKey(platform)
	lossPolicy, err := capabilitycontract.LossPolicy(key)
	if err != nil {
		return err
	}
	verification := h.verifyAllocationCapability(operationCtx, allocationID, &capabilityv1.CapabilityDependency{Key: key, LossPolicy: lossPolicy})
	if verification.State != contract.CapabilityVerificationVerified {
		return fmt.Errorf("verify %s conformance: %s", platform, verificationMessage(verification))
	}
	return nil
}

func runtimeConformanceOperationContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	operationDeadline := time.Now().Add(runtimeConformanceTimeout - runtimeConformanceCleanup)
	if deadline, ok := parent.Deadline(); ok {
		operationDeadline = deadline.Add(-runtimeConformanceCleanup)
	}
	if !operationDeadline.After(time.Now()) {
		return nil, nil, fmt.Errorf("runtime conformance cleanup reserve exhausted before sandbox start")
	}
	ctx, cancel := context.WithDeadline(parent, operationDeadline)
	return ctx, cancel, nil
}

func (h *sandboxService) cleanupRuntimeConformanceAllocation(ctx context.Context, runtimeName, allocationID string) error {
	_, err := h.allocationController().Delete(ctx, &runtimev1.DeleteRequest{ID: allocationID, Timeout: 0})
	if err == nil {
		return h.verifyRuntimeConformanceCleanup(ctx, allocationID)
	}
	if !errors.Is(err, errord.ErrNotFound) && !errord.IsNotFound(errord.FromGRPC(err)) {
		return err
	}

	// A probe can fail after creating runtime-owned storage but before manager
	// metadata becomes visible. The normal allocation delete then has no
	// ownership record, so complete the same ordered cleanup from the reserved
	// self-test identity: runtime/storage, network activation, resources/bundle,
	// and finally allocation state.
	handler, ok := h.runtimeHandlers.Get(runtimeName)
	if !ok {
		return fmt.Errorf("runtime handler %s is unavailable", runtimeName)
	}
	resource, resourceErr := h.containerManager.CollectResourceByID(allocationID)
	if resourceErr != nil && !errors.Is(resourceErr, os.ErrNotExist) {
		return fmt.Errorf("collect partial self-test resources: %w", resourceErr)
	}
	if _, deleteErr := handler.DeleteContainer(ctx, &runtimev1.DeleteContainerRequest{ID: allocationID, Timeout: 0}, contract.HandlerOptions{
		ContainerID: allocationID,
		ForceDelete: true,
	}); deleteErr != nil {
		return fmt.Errorf("delete partial self-test runtime: %w", deleteErr)
	}
	if resourceErr == nil {
		if networkErr := h.sandboxNetworking().CleanupActivationNetwork(resource); networkErr != nil {
			return fmt.Errorf("cleanup partial self-test network: %w", networkErr)
		}
	}
	if managerErr := h.containerManager.DeleteAfterConfirmedRuntimeAbsence(allocationID); managerErr != nil {
		return fmt.Errorf("cleanup partial self-test bundle: %w", managerErr)
	}
	if stateErr := h.allocationController().CleanupFailedStart(ctx, allocationID); stateErr != nil {
		return fmt.Errorf("cleanup partial self-test state: %w", stateErr)
	}
	return h.verifyRuntimeConformanceCleanup(ctx, allocationID)
}

func (h *sandboxService) verifyRuntimeConformanceCleanup(ctx context.Context, allocationID string) error {
	mode, err := h.config.PluginConfig.RuntimeConfig.CgroupEnforcementMode()
	if err != nil {
		return fmt.Errorf("resolve cgroup enforcement for self-test cleanup: %w", err)
	}
	verifyCgroup := mode == config.CgroupEnforcementRequired
	if verifyCgroup && h.containerManager == nil {
		return fmt.Errorf("verify self-test cgroup cleanup: container manager is unavailable")
	}
	paths := []string{
		filepath.Join(h.config.RootDir, "containers", allocationID),
		filepath.Join(h.config.PluginConfig.RuntimeConfig.FilestoreDir, "projections", allocationID),
		filepath.Join(h.config.PluginConfig.RuntimeConfig.FilestoreDir, "runc", allocationID),
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		remaining := ""
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				remaining = "artifact " + path
				break
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("verify self-test artifact %s: %w", path, err)
			}
		}
		if remaining == "" && verifyCgroup {
			pending, detail, err := h.containerManager.CgroupCleanupStatus(allocationID)
			if err != nil {
				return fmt.Errorf("verify self-test cgroup cleanup: %w", err)
			}
			if pending {
				remaining = "durable cgroup lease"
				if detail != "" {
					remaining += " (last cleanup error: " + detail + ")"
				}
			}
		}
		if remaining == "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("self-test cleanup did not converge; remaining %s: %w", remaining, ctx.Err())
		case <-ticker.C:
		}
	}
}

func runtimeConformanceStartRequest(allocationID, runtimeID, runtimeName, rootfs string, kind runtimeConformanceKind) (*runtimev1.StartRequest, error) {
	request := &runtimev1.StartRequest{
		ContainerID: allocationID,
		RuntimeTemplate: &runtimev1.RuntimeTemplate{
			ID:      runtimeID,
			Sandbox: runtimeName,
			Rootfs: &runtimev1.RootfsConfig{
				Type:     runtimev1.RootfsSrcType_LOCAL,
				Source:   &runtimev1.RootfsConfig_Path{Path: rootfs},
				Readonly: kind == runtimeConformanceKindMemory,
			},
			Command: []string{"/bin/busybox", "sleep", "120"},
			Cwd:     "/",
		},
	}
	switch kind {
	case runtimeConformanceKindMemory:
		request.RuntimeTemplate.Command = []string{"/bin/memory-hog"}
		request.Resources = &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{MemoryBytes: runtimeConformanceMemoryLimit},
			Limits:   &commonv1.ResourceQuantity{MemoryBytes: runtimeConformanceMemoryLimit},
		}
	case runtimeConformanceKindEphemeral:
		request.RuntimeTemplate.Command = []string{"/bin/sh", "-c", "printf 'pending\\n' > /.axern-quota-result; if /bin/busybox dd if=/dev/zero of=/.axern-quota-probe bs=1M count=96 conv=fsync; then result=not_enforced; else result=enforced; fi; rm -f /.axern-quota-probe; printf '%s\\n' \"$result\" > /.axern-quota-result; exec /bin/busybox sleep 120"}
		request.Resources = &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{EphemeralStorageBytes: runtimeConformanceStorage},
			Limits:   &commonv1.ResourceQuantity{EphemeralStorageBytes: runtimeConformanceStorage},
		}
	default:
		return nil, fmt.Errorf("unsupported runtime conformance kind %q", kind)
	}
	return request, nil
}

func runtimeConformancePlatform(runtimeName string, kind runtimeConformanceKind) (capabilityv1.PlatformCapability, error) {
	switch {
	case runtimeName == config.RuntimeNameRunc && kind == runtimeConformanceKindMemory:
		return capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT, nil
	case runtimeName == config.RuntimeNameRunsc && kind == runtimeConformanceKindMemory:
		return capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT, nil
	case runtimeName == config.RuntimeNameRunc && kind == runtimeConformanceKindEphemeral:
		return capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT, nil
	case runtimeName == config.RuntimeNameRunsc && kind == runtimeConformanceKindEphemeral:
		return capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT, nil
	default:
		return capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_UNSPECIFIED, fmt.Errorf("unsupported runtime conformance runtime=%q kind=%q", runtimeName, kind)
	}
}

func (h *sandboxService) verifyRuntimeConformanceQuotaResult(ctx context.Context, allocationID string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, err := h.ReadFile(ctx, &runtimev1.ReadFileRequest{ID: allocationID, Path: runtimeConformanceResult})
		if err == nil && response != nil {
			switch strings.TrimSpace(string(response.GetData())) {
			case "enforced":
				return nil
			case "not_enforced":
				return fmt.Errorf("runtime conformance writable storage exceeded its hard limit without ENOSPC/EDQUOT")
			case "", "pending":
				// The result inode is created before filling the overlay so the
				// final status remains writable even at the quota boundary.
			default:
				return fmt.Errorf("runtime conformance returned an invalid quota result %q", strings.TrimSpace(string(response.GetData())))
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for runtime conformance quota result: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (h *sandboxService) verifyRuntimeConformanceMemoryOOM(ctx context.Context, allocationID string) error {
	manifest := h.allocationController().EnforcementManifest(allocationID)
	if manifest == nil || manifest.GetMemoryLimitBytes() != runtimeConformanceMemoryLimit {
		return fmt.Errorf("runtime conformance memory enforcement manifest is unavailable or inconsistent")
	}
	if h.allocationController().LaunchVerification(allocationID) == nil {
		return fmt.Errorf("runtime conformance create-time memory verification is unavailable")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		ct, err := h.containerManager.Get(allocationID)
		if err != nil {
			return fmt.Errorf("read runtime conformance container status: %w", err)
		}
		if ct == nil || ct.Status == nil {
			return fmt.Errorf("runtime conformance container status is unavailable")
		}
		observation, err := hostlinux.ReadCgroupMemoryObservation(manifest.GetCgroupPath())
		if err != nil {
			return fmt.Errorf("read runtime conformance memory events: %w", err)
		}
		if observation.SwapCurrent != 0 {
			return fmt.Errorf("runtime conformance used %d bytes of swap despite memory.swap.max=0", observation.SwapCurrent)
		}
		if ct.Status.Get().State() == runtimev1.ContainerState_CONTAINER_EXITED {
			if observation.Events["oom_kill"] <= manifest.GetInitialMemoryEventOomKill() &&
				observation.Events["oom_group_kill"] <= manifest.GetInitialMemoryEventOomGroupKill() {
				return fmt.Errorf("memory-hog exited without a memcg OOM kill event")
			}
			if observation.PeakAvailable && (observation.PeakBytes <= 0 || observation.PeakBytes > manifest.GetMemoryLimitBytes()+(16<<20)) {
				return fmt.Errorf("runtime conformance memory peak %d is inconsistent with limit %d", observation.PeakBytes, manifest.GetMemoryLimitBytes())
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for runtime conformance memcg OOM: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func materializeRuntimeConformanceRootfs(filestore, fixture string) (string, error) {
	runtimeConformanceRootfsMu.Lock()
	defer runtimeConformanceRootfsMu.Unlock()

	filestore = strings.TrimSpace(filestore)
	if filestore == "" {
		return "", fmt.Errorf("runtime conformance requires filestore_dir")
	}
	destination := filepath.Join(filestore, "system", "runtime-conformance", "rootfs")
	if err := validateRuntimeConformanceRootfs(destination); err == nil {
		return destination, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("validate runtime conformance rootfs %q: %w", destination, err)
	}

	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("create runtime conformance rootfs parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".rootfs-")
	if err != nil {
		return "", fmt.Errorf("create runtime conformance rootfs staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	for _, directory := range []string{"bin", "dev", "proc", "sys", "tmp", "mnt"} {
		if err := os.Mkdir(filepath.Join(staging, directory), 0o755); err != nil {
			return "", fmt.Errorf("create runtime conformance rootfs directory %q: %w", directory, err)
		}
	}
	if err := copyRuntimeConformanceFile(
		filepath.Join(fixture, "bin", "busybox"),
		filepath.Join(staging, "bin", "busybox"),
	); err != nil {
		return "", err
	}
	if err := copyRuntimeConformanceFile(
		filepath.Join(fixture, "bin", "memory-hog"),
		filepath.Join(staging, "bin", "memory-hog"),
	); err != nil {
		return "", err
	}
	for _, name := range []string{"sh", "sleep"} {
		if err := os.Symlink("busybox", filepath.Join(staging, "bin", name)); err != nil {
			return "", fmt.Errorf("create runtime conformance symlink %q: %w", name, err)
		}
	}
	if err := syncDirectory(staging); err != nil {
		return "", fmt.Errorf("sync runtime conformance rootfs staging directory: %w", err)
	}
	quarantine := ""
	if _, err := os.Lstat(destination); err == nil {
		quarantine = filepath.Join(parent, fmt.Sprintf(".rootfs-invalid-%d", time.Now().UTC().UnixNano()))
		if err := os.Rename(destination, quarantine); err != nil {
			return "", fmt.Errorf("quarantine invalid runtime conformance rootfs: %w", err)
		}
		if err := syncDirectory(parent); err != nil {
			_ = os.Rename(quarantine, destination)
			return "", fmt.Errorf("sync quarantined runtime conformance rootfs: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect runtime conformance rootfs destination: %w", err)
	}
	if err := os.Rename(staging, destination); err != nil {
		if quarantine != "" {
			_ = os.Rename(quarantine, destination)
			_ = syncDirectory(parent)
		}
		return "", fmt.Errorf("publish runtime conformance rootfs: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return "", fmt.Errorf("sync runtime conformance rootfs parent: %w", err)
	}
	if err := removeRuntimeConformanceRootfsDebris(parent); err != nil {
		return "", err
	}
	return destination, nil
}

func removeRuntimeConformanceRootfsDebris(parent string) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("list runtime conformance rootfs parent: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".rootfs-") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
			return fmt.Errorf("remove runtime conformance rootfs debris %q: %w", entry.Name(), err)
		}
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync runtime conformance rootfs debris cleanup: %w", err)
	}
	return nil
}

func validateRuntimeConformanceRootfs(rootfs string) error {
	info, err := os.Lstat(rootfs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("rootfs is not a directory")
	}
	busybox, err := os.Lstat(filepath.Join(rootfs, "bin", "busybox"))
	if err != nil {
		return err
	}
	if !busybox.Mode().IsRegular() || busybox.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("bin/busybox is not an executable regular file")
	}
	memoryHog, err := os.Lstat(filepath.Join(rootfs, "bin", "memory-hog"))
	if err != nil {
		return err
	}
	if !memoryHog.Mode().IsRegular() || memoryHog.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("bin/memory-hog is not an executable regular file")
	}
	for _, name := range []string{"sh", "sleep"} {
		target, err := os.Readlink(filepath.Join(rootfs, "bin", name))
		if err != nil {
			return err
		}
		if target != "busybox" {
			return fmt.Errorf("bin/%s points to %q, want busybox", name, target)
		}
	}
	return nil
}

func copyRuntimeConformanceFile(source, destination string) (retErr error) {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open runtime conformance fixture %q: %w", source, err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("stat runtime conformance fixture %q: %w", source, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("runtime conformance fixture %q is not an executable regular file", source)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create runtime conformance file %q: %w", destination, err)
	}
	defer func() {
		if closeErr := output.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("close runtime conformance file %q: %w", destination, closeErr)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("copy runtime conformance file %q: %w", destination, err)
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("sync runtime conformance file %q: %w", destination, err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func firstRuntimeConformanceError(err error, message string) error {
	if err != nil {
		return err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "runtime conformance start failed without a diagnostic"
	}
	return fmt.Errorf("%s", message)
}
