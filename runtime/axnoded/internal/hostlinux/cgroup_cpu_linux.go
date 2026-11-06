//go:build linux

package hostlinux

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	cg2 "github.com/containerd/cgroups/v3/cgroup2"
)

// VerifyCgroupCPUControls reads the requested CPU controls from the allocated
// workload leaf. It never substitutes an unlimited or default control for a
// missing, unreadable, or mismatched value.
func VerifyCgroupCPUControls(cgroupPath string, resource *runtimeapi.LinuxContainerResources) error {
	if resource.GetCpuShares() == 0 && resource.GetCpuQuota() <= 0 && resource.GetCpuPeriod() == 0 {
		return nil
	}
	if strings.TrimSpace(cgroupPath) == "" {
		return fmt.Errorf("CPU controls require an allocated cgroup path")
	}
	return verifyCgroupCPUControlsDir(resourceDirForCgroupPath(cgroupPath), resource)
}

func verifyCgroupCPUControlsDir(dir string, resource *runtimeapi.LinuxContainerResources) error {
	if resource.GetCpuQuota() > 0 && resource.GetCpuPeriod() == 0 {
		return fmt.Errorf("CPU quota requires an explicit period")
	}
	if shares := resource.GetCpuShares(); shares > 0 {
		data, err := os.ReadFile(filepath.Join(dir, "cpu.weight"))
		if err != nil {
			return fmt.Errorf("read cgroup v2 cpu.weight: %w", err)
		}
		got, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return fmt.Errorf("parse cgroup v2 cpu.weight: %w", err)
		}
		want := cg2.ConvertCPUSharesToCgroupV2Value(shares)
		if got != want {
			return fmt.Errorf("CPU weight readback mismatch: got=%d want=%d", got, want)
		}
	}
	if period := resource.GetCpuPeriod(); period > 0 {
		data, err := os.ReadFile(filepath.Join(dir, "cpu.max"))
		if err != nil {
			return fmt.Errorf("read cgroup v2 cpu.max: %w", err)
		}
		fields := strings.Fields(string(data))
		if len(fields) != 2 {
			return fmt.Errorf("invalid cgroup v2 cpu.max: %q", strings.TrimSpace(string(data)))
		}
		gotPeriod, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return fmt.Errorf("parse cgroup v2 cpu.max period: %w", err)
		}
		wantQuota := "max"
		if quota := resource.GetCpuQuota(); quota > 0 {
			wantQuota = strconv.FormatInt(quota, 10)
		}
		if fields[0] != wantQuota || gotPeriod != period {
			return fmt.Errorf("CPU quota/period readback mismatch: got=%s want=%s %d", strings.TrimSpace(string(data)), wantQuota, period)
		}
	}
	return nil
}
