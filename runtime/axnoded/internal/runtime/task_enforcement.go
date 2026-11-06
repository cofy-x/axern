package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

var (
	configureTaskPIDDomain = hostlinux.ConfigureCgroupPIDDomain
	verifyTaskPIDDomain    = hostlinux.VerifyCgroupPIDDomain
	verifyTaskProcesses    = hostlinux.VerifyRunscCgroupProcesses
)

// VerifyAllocationTaskLimits audits a recovered runtime against its durable
// policy, even when the current node configuration has changed or disabled it.
// Enabling the policy cannot adopt an older unbounded live Allocation.
func (r *RunscServiceHandler) VerifyAllocationTaskLimits(ctx context.Context, containerID string, durable *apipb.AllocationEnforcementManifest) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.validateTaskEnforcementManifest(ctx, containerID, durable); err != nil {
		return err
	}
	if durable.GetGuestTaskLimit() == 0 {
		return nil
	}
	state, err := r.state(ctx, containerID)
	if err != nil {
		return fmt.Errorf("read runsc state for recovered task enforcement: %w", err)
	}
	if state.Status != string(contract.ContainerStatusRunning) {
		return fmt.Errorf("task-limited allocation is not running: %q", state.Status)
	}
	if err := r.verifyHostTaskLimits(ctx, containerID, durable.GetCgroupPath(), durable.GetRuntimeCgroupPath(), durable.GetHostTaskLimit()); err != nil {
		return err
	}
	return r.verifyGuestTaskLimits(ctx, containerID, durable.GetGuestTaskLimit())
}

func (r *RunscServiceHandler) validateTaskEnforcementManifest(ctx context.Context, containerID string, durable *apipb.AllocationEnforcementManifest) error {
	if !validPersistentContainerID(containerID) || containerID == ".." {
		return fmt.Errorf("task enforcement requires an exact allocation identity")
	}
	if err := contract.ValidateEnforcementManifest(durable, ""); err != nil {
		return fmt.Errorf("validate durable task enforcement manifest: %w", err)
	}
	if durable.GetGuestTaskLimit() == 0 {
		if r.guestTaskLimit > 0 {
			return fmt.Errorf("allocation %s predates task enforcement; drain and recreate it before enabling the policy", containerID)
		}
		return nil
	}
	manifest, err := r.AllocationEnforcementManifest(ctx, containerID)
	if err != nil {
		return fmt.Errorf("read runtime task enforcement manifest: %w", err)
	}
	return verifyDurableEnforcementManifest(durable, manifest)
}

func (r *RunscServiceHandler) verifyHostTaskLimits(ctx context.Context, containerID, parentPath, workloadPath string, limit int64) error {
	if err := verifyTaskPIDDomain(parentPath, workloadPath, limit); err != nil {
		return fmt.Errorf("verify host task domain: %w", err)
	}
	state, err := r.state(ctx, containerID)
	if err != nil {
		return fmt.Errorf("read runsc state for host task attribution: %w", err)
	}
	if state.Pid <= 0 || (state.Status != string(contract.ContainerStatusCreated) && state.Status != string(contract.ContainerStatusRunning)) {
		return fmt.Errorf("runsc has no live Sentry for host task attribution")
	}
	if err := verifyTaskProcesses(workloadPath, state.Pid, r.common.Binary()); err != nil {
		return fmt.Errorf("verify host task Sentry/gofer attribution: %w", err)
	}
	return nil
}

// configureGuestTaskLimits runs after OCI start but before sandboxd readiness.
// PID 1 waits without exposing a listener or starting caller code until it can
// independently read these exact controls. The task ceiling is written last.
func (r *RunscServiceHandler) configureGuestTaskLimits(ctx context.Context, containerID string, manifest *apipb.AllocationEnforcementManifest) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.validateTaskEnforcementManifest(ctx, containerID, manifest); err != nil {
		return err
	}
	if err := r.verifyHostTaskLimits(ctx, containerID, manifest.GetCgroupPath(), manifest.GetRuntimeCgroupPath(), manifest.GetHostTaskLimit()); err != nil {
		return err
	}
	for _, control := range []string{"cgroup.max.depth", "cgroup.max.descendants"} {
		if err := r.writeGuestTaskControl(ctx, containerID, control, "0"); err != nil {
			return err
		}
	}
	if err := r.verifyGuestTaskCounts(ctx, containerID, manifest.GetGuestTaskLimit()); err != nil {
		return err
	}
	if err := r.writeGuestTaskControl(ctx, containerID, "pids.max", strconv.FormatInt(manifest.GetGuestTaskLimit(), 10)); err != nil {
		return err
	}
	return r.verifyGuestTaskLimits(ctx, containerID, manifest.GetGuestTaskLimit())
}

func (r *RunscServiceHandler) writeGuestTaskControl(ctx context.Context, containerID, control, value string) error {
	if _, err := r.common.Run(ctx, "write-control", containerID, "pids", "/"+containerID, control, value); err != nil {
		return fmt.Errorf("write guest %s: %w", control, err)
	}
	return r.verifyGuestTaskControl(ctx, containerID, control, value)
}

func (r *RunscServiceHandler) readGuestTaskControl(ctx context.Context, containerID, control string) (string, error) {
	output, err := r.common.Run(ctx, "read-control", containerID, "pids", "/"+containerID, control)
	if err != nil {
		return "", fmt.Errorf("read guest %s: %w", control, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (r *RunscServiceHandler) verifyGuestTaskControl(ctx context.Context, containerID, control, want string) error {
	got, err := r.readGuestTaskControl(ctx, containerID, control)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("guest %s readback mismatch: got=%q want=%q", control, got, want)
	}
	return nil
}

func (r *RunscServiceHandler) verifyGuestTaskLimits(ctx context.Context, containerID string, limit int64) error {
	for _, control := range []struct{ name, want string }{
		{"cgroup.max.depth", "0"}, {"cgroup.max.descendants", "0"}, {"pids.max", strconv.FormatInt(limit, 10)},
	} {
		if err := r.verifyGuestTaskControl(ctx, containerID, control.name, control.want); err != nil {
			return err
		}
	}
	return r.verifyGuestTaskCounts(ctx, containerID, limit)
}

func (r *RunscServiceHandler) verifyGuestTaskCounts(ctx context.Context, containerID string, limit int64) error {
	current, err := r.readGuestTaskControl(ctx, containerID, "pids.current")
	if err != nil {
		return err
	}
	count, err := strconv.ParseInt(current, 10, 64)
	if err != nil || count <= 0 || count > limit || strconv.FormatInt(count, 10) != current {
		return fmt.Errorf("guest task count %q is not within the positive limit %d", current, limit)
	}
	stat, err := r.readGuestTaskControl(ctx, containerID, "cgroup.stat")
	if err != nil {
		return err
	}
	values := make(map[string]string)
	for _, line := range strings.Split(stat, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return fmt.Errorf("malformed guest cgroup.stat")
		}
		if _, duplicate := values[fields[0]]; duplicate {
			return fmt.Errorf("duplicate guest cgroup.stat field %q", fields[0])
		}
		values[fields[0]] = fields[1]
	}
	if values["nr_descendants"] != "0" || values["nr_dying_descendants"] != "0" {
		return fmt.Errorf("guest task domain has descendants or incomplete descendant evidence")
	}
	return nil
}
