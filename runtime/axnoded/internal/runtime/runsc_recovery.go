package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

var killRecoveryCgroup = hostlinux.KillRunscCgroup
var recoveryCgroupStopped = hostlinux.RunscCgroupStopped

// PauseContainer stops guest scheduling while leaving Sentry's host control
// socket available for enforcement audits and fail-stop during node downtime.
func (r *RunscServiceHandler) PauseContainer(ctx context.Context, options contract.HandlerOptions) error {
	return r.transitionRecoveryState(ctx, options.ContainerID, "pause", contract.ContainerStatusRunning, contract.ContainerStatusPaused)
}

func (r *RunscServiceHandler) ResumeContainer(ctx context.Context, options contract.HandlerOptions) error {
	if !options.GuestProcessProfileVerified {
		return fmt.Errorf("allocation has no observed guest process profile; stop and recreate its execution")
	}
	if err := r.verifyManagedProcessPolicy(ctx, options.ContainerID, options.EnforcementManifest); err != nil {
		return err
	}
	return r.transitionRecoveryState(ctx, options.ContainerID, "resume", contract.ContainerStatusPaused, contract.ContainerStatusRunning)
}

func (r *RunscServiceHandler) transitionRecoveryState(parent context.Context, id, command string, from, to contract.ContainerStatus) error {
	if !r.nativeRecovery {
		return fmt.Errorf("native recovery is disabled")
	}
	if !validPersistentContainerID(id) || id == ".." {
		return fmt.Errorf("%s requires an exact allocation identity", command)
	}
	ctx, cancel := context.WithTimeout(parent, runscForceStopTimeout)
	defer cancel()
	r.recoveryTransitionMu.Lock()
	defer r.recoveryTransitionMu.Unlock()
	fence := filepath.Join(filepath.Dir(r.containerRoot), "runtime-transition")
	if _, err := os.Lstat(fence); err == nil {
		return fmt.Errorf("unfinished runtime transition requires native supervisor fail-stop")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect runtime transition fence: %w", err)
	}
	state, err := r.state(ctx, id)
	if err != nil {
		return fmt.Errorf("read runtime before %s: %w", command, err)
	}
	if state.Status == string(to) {
		return nil
	}
	if command == "pause" && (state.Status == string(contract.ContainerStatusCreated) || state.Status == string(contract.ContainerStatusExited)) {
		return nil
	}
	if state.Status != string(from) {
		return fmt.Errorf("cannot %s runtime in state %q", command, state.Status)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := createRuntimeTransitionFence(fence); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, os.Remove(fence), syncRuntimeTransitionDirectory(filepath.Dir(fence)))
	}
	if _, err := r.runLifecycle(ctx, command, id); err != nil {
		return fmt.Errorf("%s runtime: %w", command, err)
	}
	state, err = r.state(ctx, id)
	if err != nil {
		return fmt.Errorf("confirm runtime after %s: %w", command, err)
	}
	if state.Status != string(to) {
		return fmt.Errorf("runtime after %s is %q, expected %q", command, state.Status, to)
	}
	if err := os.Remove(fence); err != nil {
		return fmt.Errorf("clear runtime transition fence: %w", err)
	}
	return syncRuntimeTransitionDirectory(filepath.Dir(fence))
}

// The native stop hook uses this single fail-stop marker too. It is not
// execution authority: it records that runsc's RPC and persisted OCI state
// might disagree. An interrupted transition always causes host fail-stop.
func createRuntimeTransitionFence(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create runtime transition fence: %w", err)
	}
	_, writeErr := file.WriteString("v1\n")
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("persist runtime transition fence: %w", err)
	}
	return syncRuntimeTransitionDirectory(filepath.Dir(path))
}

func syncRuntimeTransitionDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open runtime transition directory: %w", err)
	}
	return errors.Join(dir.Sync(), dir.Close())
}

// KillSandbox is the host-side fail-stop for a paused guest whose execution
// authority expired. It deliberately leaves the OCI state, writable rootfs,
// and resource claims intact for terminal checkpointing and ordered cleanup.
func (r *RunscServiceHandler) KillSandbox(parent context.Context, options contract.HandlerOptions) error {
	id := options.ContainerID
	if !validPersistentContainerID(id) || id == ".." {
		return fmt.Errorf("sandbox fail-stop requires an exact allocation identity")
	}
	ctx, cancel := context.WithTimeout(parent, runscForceStopTimeout)
	defer cancel()
	state, err := r.state(ctx, id)
	if err != nil {
		return fmt.Errorf("read runtime before fail-stop: %w", err)
	}
	if state.Status == string(contract.ContainerStatusExited) {
		return nil
	}
	if state.Status == string(contract.ContainerStatusPaused) || (r.nativeRecovery && options.EnforcementManifest != nil) {
		manifest := options.EnforcementManifest
		if err := r.validateRecoveryManifest(ctx, id, manifest); err != nil {
			return err
		}
		stopped, err := r.VerifySandboxStopped(ctx, options)
		if err != nil {
			return err
		}
		if stopped {
			return nil
		}
		if err := killRecoveryCgroup(manifest.GetRuntimeCgroupPath(), state.Pid, r.common.Binary(), manifest.GetCgroupBootID(), manifest.GetCgroupMountIdentity(), manifest.GetCgroupLeafInode()); err != nil {
			return err
		}
	} else if _, err := r.runLifecycle(ctx, "kill", id, "KILL"); err != nil {
		return fmt.Errorf("fail-stop runtime: %w", err)
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		stopped, err := r.VerifySandboxStopped(ctx, options)
		if err != nil {
			return fmt.Errorf("confirm runtime fail-stop: %w", err)
		}
		if stopped {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("confirm runtime fail-stop: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *RunscServiceHandler) validateRecoveryManifest(ctx context.Context, id string, manifest *apipb.AllocationEnforcementManifest) error {
	if err := contract.ValidateEnforcementManifest(manifest, filepath.Join(r.containerRoot, id)); err != nil {
		return fmt.Errorf("validate sandbox recovery authority: %w", err)
	}
	projected, err := r.AllocationEnforcementManifest(ctx, id)
	if err != nil {
		return err
	}
	return verifyDurableEnforcementManifest(manifest, projected)
}

// VerifySandboxStopped never interprets stale paused status as execution proof.
// The caller supplies the sole admitted manifest before any recovery cleanup.
func (r *RunscServiceHandler) VerifySandboxStopped(ctx context.Context, options contract.HandlerOptions) (bool, error) {
	id := options.ContainerID
	if !validPersistentContainerID(id) || id == ".." {
		return false, fmt.Errorf("sandbox termination verification requires exact allocation identity")
	}
	state, err := r.state(ctx, id)
	if err != nil {
		return false, err
	}
	if state.Status == string(contract.ContainerStatusExited) {
		return true, nil
	}
	if state.Status != string(contract.ContainerStatusPaused) && !(r.nativeRecovery && options.EnforcementManifest != nil) {
		return false, nil
	}
	manifest := options.EnforcementManifest
	if err := r.validateRecoveryManifest(ctx, id, manifest); err != nil {
		return false, err
	}
	return recoveryCgroupStopped(manifest.GetRuntimeCgroupPath(), state.Pid, manifest.GetCgroupBootID(), manifest.GetCgroupMountIdentity(), manifest.GetCgroupLeafInode())
}

// startWithTransitionFence closes start-RPC versus persisted-created ambiguity
// before the native stop hook classifies a never-started container as harmless.
func (r *RunscServiceHandler) startWithTransitionFence(ctx context.Context, id string, args []string) error {
	r.recoveryTransitionMu.Lock()
	defer r.recoveryTransitionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	fence := filepath.Join(filepath.Dir(r.containerRoot), "runtime-transition")
	if err := createRuntimeTransitionFence(fence); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, os.Remove(fence), syncRuntimeTransitionDirectory(filepath.Dir(fence)))
	}
	if _, err := r.common.Run(ctx, args...); err != nil {
		return err
	}
	state, err := r.state(ctx, id)
	if err != nil {
		return err
	}
	if state.Status != string(contract.ContainerStatusRunning) && state.Status != string(contract.ContainerStatusExited) {
		return fmt.Errorf("start did not publish a started runtime state: %q", state.Status)
	}
	if err := os.Remove(fence); err != nil {
		return err
	}
	return syncRuntimeTransitionDirectory(filepath.Dir(fence))
}
