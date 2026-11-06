//go:build linux

package hostlinux

import (
	"os"
	"path/filepath"
	"testing"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"google.golang.org/protobuf/proto"
)

func TestVerifyCgroupCPUControls(t *testing.T) {
	for _, test := range []struct {
		name     string
		resource *runtimeapi.LinuxContainerResources
		weight   string
		maximum  string
		wantErr  bool
	}{
		{name: "CPU only", resource: &runtimeapi.LinuxContainerResources{CpuShares: 1024, CpuQuota: 25000, CpuPeriod: 100000}, weight: "100\n", maximum: "25000 100000\n"},
		{name: "quota without weight", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, maximum: "25000 100000\n"},
		{name: "weight without quota", resource: &runtimeapi.LinuxContainerResources{CpuShares: 1024}, weight: "100\n"},
		{name: "explicit unlimited period", resource: &runtimeapi.LinuxContainerResources{CpuPeriod: 100000}, maximum: "max 100000\n"},
		{name: "missing weight", resource: &runtimeapi.LinuxContainerResources{CpuShares: 1024, CpuQuota: 25000, CpuPeriod: 100000}, maximum: "25000 100000\n", wantErr: true},
		{name: "mismatched weight", resource: &runtimeapi.LinuxContainerResources{CpuShares: 1024}, weight: "99\n", wantErr: true},
		{name: "malformed weight", resource: &runtimeapi.LinuxContainerResources{CpuShares: 1024}, weight: "invalid\n", wantErr: true},
		{name: "missing maximum", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, wantErr: true},
		{name: "unlimited maximum", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, maximum: "max 100000\n", wantErr: true},
		{name: "mismatched quota", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, maximum: "50000 100000\n", wantErr: true},
		{name: "same ratio different period", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, maximum: "50000 200000\n", wantErr: true},
		{name: "malformed maximum", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, maximum: "25000\n", wantErr: true},
		{name: "extra maximum fields", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, maximum: "25000 100000 extra\n", wantErr: true},
		{name: "malformed period", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, maximum: "25000 invalid\n", wantErr: true},
		{name: "zero quota", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, maximum: "0 100000\n", wantErr: true},
		{name: "negative quota", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}, maximum: "-1 100000\n", wantErr: true},
		{name: "quota requires period", resource: &runtimeapi.LinuxContainerResources{CpuQuota: 25000}, maximum: "25000 100000\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, value := range map[string]string{"cpu.weight": test.weight, "cpu.max": test.maximum} {
				if value != "" {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := verifyCgroupCPUControlsDir(dir, test.resource); (err != nil) != test.wantErr {
				t.Fatalf("verifyCgroupCPUControlsDir() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestVerifyCgroupCPUControlsDoesNotReadUnrequestedControls(t *testing.T) {
	for _, resource := range []*runtimeapi.LinuxContainerResources{nil, {}, {MemoryLimitInBytes: 1024}} {
		if err := VerifyCgroupCPUControls("", resource); err != nil {
			t.Fatalf("no requested CPU controls: %v", err)
		}
	}
	if err := VerifyCgroupCPUControls("", &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}); err == nil {
		t.Fatal("requested CPU limit accepted without an allocated cgroup")
	}
}

func TestVerifyCgroupCPUControlsRejectsUnreadableControls(t *testing.T) {
	for _, name := range []string{"cpu.weight", "cpu.max"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
				t.Fatal(err)
			}
			resource := &runtimeapi.LinuxContainerResources{CpuQuota: 25000, CpuPeriod: 100000}
			if name == "cpu.weight" {
				resource = &runtimeapi.LinuxContainerResources{CpuShares: 1024}
			}
			if err := verifyCgroupCPUControlsDir(dir, resource); err == nil {
				t.Fatal("unreadable CPU control was accepted")
			}
		})
	}
}

func TestSanitizeResourcePreservesCPUWhenControllerIsMissing(t *testing.T) {
	resource := &runtimeapi.LinuxContainerResources{CpuShares: 1024, CpuQuota: 25000, CpuPeriod: 100000, CpusetCpus: "1", CpusetMems: "0"}
	got := SanitizeResourceForDriver(&fakeCgroupDriver{}, "/axern-test-missing-cpu-controller", resource)
	if !proto.Equal(got, resource) {
		t.Fatalf("missing controller discarded requested CPU controls: got %v want %v", got, resource)
	}
}
