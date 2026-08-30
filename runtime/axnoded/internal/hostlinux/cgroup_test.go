package hostlinux

import (
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"testing"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type fakeCgroup struct{}

func (f *fakeCgroup) Update(resources *specs.LinuxResources) error { return nil }
func (f *fakeCgroup) Delete() error                                { return nil }
func (f *fakeCgroup) Stats() (*os2.CgroupStats, error)             { return &os2.CgroupStats{}, nil }
func (f *fakeCgroup) AddProc(pid uint64) error                     { return nil }
func (f *fakeCgroup) Processes(recursive bool) ([]int, error)      { return nil, nil }

type memoryProbeCgroup struct {
	limit int64
}

func (c *memoryProbeCgroup) Update(resources *specs.LinuxResources) error {
	if resources == nil || resources.Memory == nil || resources.Memory.Limit == nil {
		return fmt.Errorf("memory limit is missing")
	}
	c.limit = *resources.Memory.Limit
	return nil
}
func (c *memoryProbeCgroup) Delete() error                    { return nil }
func (c *memoryProbeCgroup) Stats() (*os2.CgroupStats, error) { return &os2.CgroupStats{}, nil }
func (c *memoryProbeCgroup) AddProc(uint64) error             { return nil }
func (c *memoryProbeCgroup) Processes(bool) ([]int, error)    { return nil, nil }

type memoryProbeDriver struct {
	cgroup  *memoryProbeCgroup
	created string
	removed []string
	mode    string
}

func (d *memoryProbeDriver) Mode() string {
	if d.mode == "" {
		return os2.CgroupModeV2
	}
	return d.mode
}
func (d *memoryProbeDriver) EnsureRoot(string, int64) error              { return nil }
func (d *memoryProbeDriver) ResolveRoot(rootName string) (string, error) { return rootName, nil }
func (d *memoryProbeDriver) Create(group string, _ *specs.LinuxResources) (os2.Cgroup, error) {
	d.created = group
	d.cgroup = &memoryProbeCgroup{}
	return d.cgroup, nil
}
func (d *memoryProbeDriver) Load(string) (os2.Cgroup, error)         { return d.cgroup, nil }
func (d *memoryProbeDriver) ExistingGroups(string) ([]string, error) { return nil, nil }
func (d *memoryProbeDriver) Remove(group string) error {
	d.removed = append(d.removed, group)
	return nil
}
func (d *memoryProbeDriver) LocalCPUCount() (int, error) { return 1, nil }

type fakeCgroupDriver struct{}

func (f *fakeCgroupDriver) Mode() string                                { return os2.CgroupModeV2 }
func (f *fakeCgroupDriver) EnsureRoot(string, int64) error              { return nil }
func (f *fakeCgroupDriver) ResolveRoot(rootName string) (string, error) { return rootName, nil }
func (f *fakeCgroupDriver) Create(group string, resources *specs.LinuxResources) (os2.Cgroup, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeCgroupDriver) Load(group string) (os2.Cgroup, error) {
	return &fakeCgroup{}, nil
}
func (f *fakeCgroupDriver) ExistingGroups(rootName string) ([]string, error) { return nil, nil }
func (f *fakeCgroupDriver) Remove(group string) error                        { return nil }
func (f *fakeCgroupDriver) LocalCPUCount() (int, error)                      { return 1, nil }

func TestRuntimeCgroupPathUsesWorkloadLeafOnV2(t *testing.T) {
	expected := "/sandbox/test/workload"
	if goruntime.GOOS != "linux" {
		expected = "/sandbox/test"
	}

	if got := RuntimeCgroupPath(&fakeCgroupDriver{}, "/sandbox/test"); got != expected {
		t.Fatalf("RuntimeCgroupPath() = %q, want %q", got, expected)
	}
}

func TestProbeCgroupMemoryLimitWritesReadsAndCleansUp(t *testing.T) {
	driver := &memoryProbeDriver{}
	verifiedPath := ""
	err := probeCgroupMemoryLimit(driver, "sandbox", func(path string, limit int64) error {
		verifiedPath = path
		if limit != cgroupMemoryProbeLimitBytes || driver.cgroup.limit != limit {
			return fmt.Errorf("limit mismatch: verify=%d written=%d", limit, driver.cgroup.limit)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if driver.created == "" || verifiedPath != RuntimeCgroupPath(driver, driver.created) {
		t.Fatalf("created=%q verified=%q", driver.created, verifiedPath)
	}
	if len(driver.removed) != 1 || driver.removed[0] != driver.created {
		t.Fatalf("cleanup calls = %#v", driver.removed)
	}
}

func TestProbeCgroupMemoryLimitRejectsNonV2Driver(t *testing.T) {
	driver := &memoryProbeDriver{mode: "v1"}
	err := probeCgroupMemoryLimit(driver, "sandbox", func(string, int64) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "requires cgroup v2") {
		t.Fatalf("probeCgroupMemoryLimit() error = %v, want cgroup v2 rejection", err)
	}
	if driver.created != "" {
		t.Fatalf("probe created cgroup %q before rejecting v1", driver.created)
	}
}

func TestUpdateCgroup(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("requires Linux cgroup filesystem")
	}
	if os.Getuid() != 0 {
		t.Skip("requires root to create cgroups")
	}

	driver, err := os2.DefaultCgroupDriver()
	if err != nil {
		t.Fatalf("load cgroup driver: %v", err)
	}

	const cgroupPath = "test-update-cgroup"
	if err := UpdateCgroup(cgroupPath, nil); err != nil {
		t.Fatalf("UpdateCgroup(nil) error = %v", err)
	}

	cgroup, err := driver.Create(cgroupPath, &specs.LinuxResources{})
	if err != nil && isCgroupEnvironmentError(err) {
		t.Skipf("cgroup controller is not writable in this environment: %v", err)
	}
	if err != nil {
		t.Fatalf("create cgroup: %v", err)
	}
	if cgroup == nil {
		t.Skip("cgroup driver did not return a cgroup handle in this environment")
	}
	defer func() {
		_ = cgroup.Delete()
	}()

	resource := runtimeapi.LinuxContainerResources{
		CpuShares:          128,
		CpuPeriod:          10000,
		CpuQuota:           8000,
		CpusetCpus:         "0",
		CpusetMems:         "0",
		MemoryLimitInBytes: 268435456,
	}

	err = UpdateCgroup(cgroupPath, &resource)
	if err != nil && isCgroupEnvironmentError(err) {
		t.Skipf("cgroup controller is not writable in this environment: %v", err)
	}
	if err != nil {
		t.Fatalf("UpdateCgroup() error = %v", err)
	}
}

func isCgroupEnvironmentError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "read-only file system") ||
		strings.Contains(msg, "resource busy")
}
