package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func (r *RuncServiceHandler) verifyMemoryEnforcement(ctx context.Context, options contract.HandlerOptions) error {
	if options.MemoryLimitBytes <= 0 {
		return nil
	}
	state, err := r.state(ctx, options.ContainerID)
	if err != nil {
		return fmt.Errorf("read runc state for memory enforcement: %w", err)
	}
	if state.Pid == nil || *state.Pid <= 0 {
		return fmt.Errorf("runc state has no host pid for memory enforcement")
	}
	if err := hostlinux.VerifyCgroupPIDs(options.CgroupPath, *state.Pid, 1); err != nil {
		return err
	}
	return recordVerifiedCapability(r.capabilityDir, "runtime-runc-memory-hard-limit")
}

func (r *RunscServiceHandler) verifyMemoryEnforcement(ctx context.Context, options contract.HandlerOptions) error {
	if options.MemoryLimitBytes <= 0 {
		return nil
	}
	state, err := r.state(ctx, options.ContainerID)
	if err != nil {
		return fmt.Errorf("read runsc state for memory enforcement: %w", err)
	}
	if state.Pid <= 0 {
		return fmt.Errorf("runsc state has no Sentry host pid for memory enforcement")
	}
	if err := hostlinux.VerifyRunscCgroupProcesses(options.CgroupPath, state.Pid); err != nil {
		return fmt.Errorf("verify runsc Sentry/gofer cgroup attribution: %w", err)
	}
	return recordVerifiedCapability(r.capabilityDir, "runtime-runsc-memory-hard-limit")
}

func recordVerifiedCapability(dir, name string) error {
	if dir == "" {
		return fmt.Errorf("verified capability directory is required")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	bootID, err := hostlinux.CurrentBootID()
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".capability-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0644); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.WriteString(bootID + "\n"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(dir, name)); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
