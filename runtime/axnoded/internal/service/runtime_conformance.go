package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	runtimeConformanceFixture = "/opt/axern/runtime-selftest/rootfs"
	runtimeConformanceResult  = "/.axern-quota-result"
	runtimeConformancePeriod  = 15 * time.Minute
	runtimeConformanceTimeout = 60 * time.Second
	// Storage exhaustion must not be mistaken for a cgroup OOM. The quota probe
	// writes beyond the 64 MiB overlay limit, so give the same sandbox enough
	// memory for runtime overhead and charged file-backed page cache while still
	// verifying that this explicit hard limit is installed and attributed.
	runtimeConformanceMemory  = 256 << 20
	runtimeConformanceStorage = 64 << 20
)

type runtimeConformanceProbe func(context.Context, string) error

type runtimeConformanceProvider struct {
	mu                sync.Mutex
	cfg               config.Config
	registry          *handlerregistry.Registry
	runtime           string
	provider          capabilityv1.CapabilityProvider
	expected          []*capabilityv1.CapabilityKey
	bootID            string
	probe             runtimeConformanceProbe
	identity          string
	lastProbe         time.Time
	nextProbe         time.Time
	failures          int
	recoverySuccesses int
	lastErr           error
	lastErrorUnknown  bool
}

func runtimeConformanceCapabilityProvider(cfg config.Config, registry *handlerregistry.Registry, runtimeName, bootID string, probe runtimeConformanceProbe) *runtimeConformanceProvider {
	provider := capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST
	memoryFact := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST
	ephemeralFact := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST
	if runtimeName == config.RuntimeNameRunsc {
		provider = capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST
		memoryFact = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST
		ephemeralFact = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST
	}
	return &runtimeConformanceProvider{
		cfg: cfg, registry: registry, runtime: runtimeName, provider: provider, bootID: bootID, probe: probe,
		expected: []*capabilityv1.CapabilityKey{capabilitycontract.PlatformKey(memoryFact), capabilitycontract.PlatformKey(ephemeralFact)},
	}
}

func (p *runtimeConformanceProvider) Provider() capabilityv1.CapabilityProvider { return p.provider }
func (p *runtimeConformanceProvider) ExpectedKeys() []*capabilityv1.CapabilityKey {
	result := make([]*capabilityv1.CapabilityKey, 0, len(p.expected))
	for _, key := range p.expected {
		result = append(result, capabilitycontract.CloneKey(key))
	}
	return result
}

func (p *runtimeConformanceProvider) Observe(ctx context.Context, now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	identity, binaryDigest, configDigest, err := p.runtimeIdentity()
	identityChanged := err == nil && identity != p.identity
	probeDue := p.lastProbe.IsZero() || (!p.nextProbe.IsZero() && !now.Before(p.nextProbe)) || (p.nextProbe.IsZero() && now.Sub(p.lastProbe) >= runtimeConformancePeriod)
	if err == nil && (identityChanged || probeDue) {
		probeCtx, cancel := context.WithTimeout(ctx, runtimeConformanceTimeout)
		err = p.probe(probeCtx, p.runtime)
		cancel()
		p.identity = identity
		p.lastProbe = now
		p.lastErr = err
		p.lastErrorUnknown = false
		if err != nil {
			p.failures++
			p.recoverySuccesses = 0
			p.nextProbe = now.Add(runtimeProbeRetryDelay(p.failures))
		} else {
			if p.failures > 0 || p.recoverySuccesses > 0 {
				p.recoverySuccesses++
			}
			p.failures = 0
			if p.recoverySuccesses == 1 {
				p.nextProbe = now.Add(5 * time.Second)
			} else {
				p.recoverySuccesses = 0
				p.nextProbe = time.Time{}
			}
		}
	} else if err != nil && (p.lastProbe.IsZero() || p.nextProbe.IsZero() || !now.Before(p.nextProbe)) {
		p.identity = identity
		p.lastProbe = now
		p.lastErr = err
		p.lastErrorUnknown = true
		p.failures++
		p.recoverySuccesses = 0
		p.nextProbe = now.Add(runtimeProbeRetryDelay(p.failures))
	}
	evidence := &capabilityv1.CapabilityEvidence{
		BootID: p.bootID, RuntimeName: p.runtime, RuntimeBinaryDigest: binaryDigest, ConfigDigest: configDigest,
	}
	if p.lastErr != nil {
		observations := failedObservationsForRuntime(p.expected, evidence, p.lastErr.Error(), p.lastErrorUnknown)
		setObservationTime(observations, p.lastProbe)
		return observations, nil
	}
	result := make([]*capabilityv1.CapabilityObservation, 0, len(p.expected))
	for _, key := range p.expected {
		result = append(result, availableObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, evidence))
	}
	setObservationTime(result, p.lastProbe)
	return result, nil
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
	binaryDigest, err = digestFile(runtimeBinary)
	if err != nil {
		return "", "", "", fmt.Errorf("digest runtime binary: %w", err)
	}
	baseSpecDigest, err := digestFile(runtimeCfg.BaseSpec)
	if err != nil {
		return "", "", "", fmt.Errorf("digest runtime base spec: %w", err)
	}
	runner, err := exec.LookPath(p.cfg.PluginConfig.RuntimeConfig.RuntimeRunnerBinaryPath())
	if err != nil {
		return "", "", "", fmt.Errorf("resolve runtime runner binary: %w", err)
	}
	runnerDigest, err := digestFile(runner)
	if err != nil {
		return "", "", "", fmt.Errorf("digest runtime runner binary: %w", err)
	}
	options, err := json.Marshal(runtimeCfg.Options)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal runtime options: %w", err)
	}
	configPayload := strings.Join([]string{baseSpecDigest, runnerDigest, string(options)}, "\x00")
	digest := sha256.Sum256([]byte(configPayload))
	configDigest = hex.EncodeToString(digest[:])
	return binaryDigest + ":" + configDigest, binaryDigest, configDigest, nil
}

func digestFile(path string) (string, error) {
	payload, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func failedObservationsForRuntime(keys []*capabilityv1.CapabilityKey, evidence *capabilityv1.CapabilityEvidence, reason string, unknown bool) []*capabilityv1.CapabilityObservation {
	result := make([]*capabilityv1.CapabilityObservation, 0, len(keys))
	for _, key := range keys {
		if unknown {
			result = append(result, unknownCapabilityObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, reason))
		} else {
			result = append(result, failedObservation(key, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, evidence, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, reason))
		}
	}
	return result
}

func (h *sandboxService) runRuntimeConformanceSelfTest(ctx context.Context, runtimeName string) (retErr error) {
	rootfs, err := materializeRuntimeConformanceRootfs(
		h.config.PluginConfig.RuntimeConfig.FilestoreDir,
		runtimeConformanceFixture,
	)
	if err != nil {
		return err
	}
	runtimeID := "capability-selftest-" + runtimeName
	allocationID := runtimeID + "-" + uuid.NewString()
	request := &runtimev1.StartRequest{
		ContainerID: allocationID,
		RuntimeTemplate: &runtimev1.RuntimeTemplate{
			ID:      runtimeID,
			Sandbox: runtimeName,
			Rootfs: &runtimev1.RootfsConfig{
				Type:     runtimev1.RootfsSrcType_LOCAL,
				Source:   &runtimev1.RootfsConfig_Path{Path: rootfs},
				Readonly: false,
			},
			Command: []string{"/bin/sh", "-c", "printf 'pending\\n' > /.axern-quota-result; if /bin/busybox dd if=/dev/zero of=/.axern-quota-probe bs=1M count=96 conv=fsync; then result=not_enforced; else result=enforced; fi; rm -f /.axern-quota-probe; printf '%s\\n' \"$result\" > /.axern-quota-result; exec /bin/busybox sleep 120"},
			Cwd:     "/",
		},
		Resources: &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{MemoryBytes: runtimeConformanceMemory, EphemeralStorageBytes: runtimeConformanceStorage},
			Limits:   &commonv1.ResourceQuantity{MemoryBytes: runtimeConformanceMemory, EphemeralStorageBytes: runtimeConformanceStorage},
		},
	}
	startAttempted := false
	defer func() {
		if !startAttempted {
			return
		}
		deleteCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := h.allocationController().Delete(deleteCtx, &runtimev1.DeleteRequest{ID: allocationID, Timeout: 0}); err != nil {
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
	response, err := h.allocationController().Start(ctx, request)
	if err != nil || response == nil || response.GetCode() != 0 {
		message := "empty response"
		if response != nil {
			message = response.GetMessage()
		}
		return fmt.Errorf("start runtime conformance sandbox: %w", firstRuntimeConformanceError(err, message))
	}
	if err := h.verifyRuntimeConformanceQuotaResult(ctx, allocationID); err != nil {
		return err
	}
	platforms := []capabilityv1.PlatformCapability{
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT,
	}
	if runtimeName == config.RuntimeNameRunsc {
		platforms = []capabilityv1.PlatformCapability{
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT,
		}
	}
	for _, platform := range platforms {
		key := capabilitycontract.PlatformKey(platform)
		lossPolicy, policyErr := capabilitycontract.LossPolicy(key)
		if policyErr != nil {
			return policyErr
		}
		verification := h.verifyAllocationCapability(ctx, allocationID, &capabilityv1.CapabilityDependency{Key: key, LossPolicy: lossPolicy})
		if verification.State != contract.CapabilityVerificationVerified {
			return fmt.Errorf("verify %s conformance: %s", platform, verificationMessage(verification))
		}
	}
	return nil
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

func materializeRuntimeConformanceRootfs(filestore, fixture string) (string, error) {
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
	for _, name := range []string{"sh", "sleep"} {
		if err := os.Symlink("busybox", filepath.Join(staging, "bin", name)); err != nil {
			return "", fmt.Errorf("create runtime conformance symlink %q: %w", name, err)
		}
	}
	if err := syncDirectory(staging); err != nil {
		return "", fmt.Errorf("sync runtime conformance rootfs staging directory: %w", err)
	}
	if err := os.Rename(staging, destination); err != nil {
		if validateErr := validateRuntimeConformanceRootfs(destination); validateErr == nil {
			return destination, nil
		}
		return "", fmt.Errorf("publish runtime conformance rootfs: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return "", fmt.Errorf("sync runtime conformance rootfs parent: %w", err)
	}
	return destination, nil
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
