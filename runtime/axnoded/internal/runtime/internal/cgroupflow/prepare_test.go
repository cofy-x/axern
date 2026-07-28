package cgroupflow

import (
	"errors"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type fakeCgroupDriver struct{}

func (fakeCgroupDriver) Mode() string { return os2.CgroupModeV2 }
func (fakeCgroupDriver) Create(string, *specs.LinuxResources) (os2.Cgroup, error) {
	return nil, nil
}
func (fakeCgroupDriver) Load(string) (os2.Cgroup, error)         { return nil, nil }
func (fakeCgroupDriver) ExistingGroups(string) ([]string, error) { return nil, nil }
func (fakeCgroupDriver) Remove(string) error                     { return nil }
func (fakeCgroupDriver) LocalCPUCount() (int, error)             { return 1, nil }

func withCgroupRuntimeHooks(t *testing.T, updateErr error, writePermissionErr bool) {
	t.Helper()

	oldDefaultCgroupDriver := defaultCgroupDriver
	oldRuntimeCgroupPath := runtimeCgroupPath
	oldSanitizeResourceForDriver := sanitizeResourceForDriver
	oldUpdateCgroup := updateCgroup
	oldIsCgroupWritePermissionErr := isCgroupWritePermissionErr
	t.Cleanup(func() {
		defaultCgroupDriver = oldDefaultCgroupDriver
		runtimeCgroupPath = oldRuntimeCgroupPath
		sanitizeResourceForDriver = oldSanitizeResourceForDriver
		updateCgroup = oldUpdateCgroup
		isCgroupWritePermissionErr = oldIsCgroupWritePermissionErr
	})

	defaultCgroupDriver = func() (os2.CgroupDriver, error) {
		return fakeCgroupDriver{}, nil
	}
	runtimeCgroupPath = func(os2.CgroupDriver, string) string {
		return "/sandbox/test/workload"
	}
	sanitizeResourceForDriver = func(_ os2.CgroupDriver, _ string, resource *apipb.LinuxContainerResources) *apipb.LinuxContainerResources {
		return resource
	}
	updateCgroup = func(string, *apipb.LinuxContainerResources) error {
		return updateErr
	}
	isCgroupWritePermissionErr = func(error) bool {
		return writePermissionErr
	}
}

func TestPrepareInactiveWhenCgroupPathEmpty(t *testing.T) {
	prep, err := Prepare(&apipb.CreateContainerRequest{
		Resource: &apipb.LinuxContainerResources{},
	}, "")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prep.Active {
		t.Fatalf("Prepare().Active = true, want false")
	}
	if prep.RuntimeCgroupPath != "" {
		t.Fatalf("Prepare().RuntimeCgroupPath = %q, want empty", prep.RuntimeCgroupPath)
	}
	if prep.SanitizedResource != nil {
		t.Fatalf("Prepare().SanitizedResource = %#v, want nil", prep.SanitizedResource)
	}
}

func TestPrepareRuntimeClearsCgroupOptionsWhenIgnored(t *testing.T) {
	request := &apipb.CreateContainerRequest{
		Resource: &apipb.LinuxContainerResources{MemoryLimitInBytes: 1024},
	}

	prep, err := PrepareRuntime(request, contract.HandlerOptions{
		CgroupPath:        "/sandbox/test",
		RuntimeCgroupPath: "/sandbox/test/workload",
	}, RuntimePolicy{IgnoreCgroups: true})
	if err != nil {
		t.Fatalf("PrepareRuntime() error = %v", err)
	}
	if prep.Request != request {
		t.Fatalf("PrepareRuntime().Request = cloned request, want original")
	}
	if prep.Options.CgroupPath != "" {
		t.Fatalf("PrepareRuntime().Options.CgroupPath = %q, want empty", prep.Options.CgroupPath)
	}
	if prep.Options.RuntimeCgroupPath != "" {
		t.Fatalf("PrepareRuntime().Options.RuntimeCgroupPath = %q, want empty", prep.Options.RuntimeCgroupPath)
	}
}

func TestPrepareRuntimeDropsResourceWhenIgnoredPolicyRequiresIt(t *testing.T) {
	request := &apipb.CreateContainerRequest{
		Resource: &apipb.LinuxContainerResources{MemoryLimitInBytes: 1024},
	}

	prep, err := PrepareRuntime(request, contract.HandlerOptions{
		CgroupPath:        "/sandbox/test",
		RuntimeCgroupPath: "/sandbox/test/workload",
	}, RuntimePolicy{IgnoreCgroups: true, DropResourceWhenIgnored: true})
	if err != nil {
		t.Fatalf("PrepareRuntime() error = %v", err)
	}
	if prep.Request == request {
		t.Fatalf("PrepareRuntime().Request = original request, want clone")
	}
	if prep.Request.Resource != nil {
		t.Fatalf("PrepareRuntime().Request.Resource = %#v, want nil", prep.Request.Resource)
	}
	if request.Resource == nil {
		t.Fatalf("PrepareRuntime() mutated original request resource")
	}
	if prep.Options.CgroupPath != "" || prep.Options.RuntimeCgroupPath != "" {
		t.Fatalf("PrepareRuntime().Options cgroups = %q/%q, want empty", prep.Options.CgroupPath, prep.Options.RuntimeCgroupPath)
	}
}

func TestPrepareRuntimeFailsClosedWhenCgroupUpdateFails(t *testing.T) {
	updateErr := errors.New("cgroup update failed")
	withCgroupRuntimeHooks(t, updateErr, false)

	_, err := PrepareRuntime(&apipb.CreateContainerRequest{
		Resource: &apipb.LinuxContainerResources{MemoryLimitInBytes: 1024},
	}, contract.HandlerOptions{CgroupPath: "/sandbox/test"}, RuntimePolicy{})
	if err == nil {
		t.Fatal("PrepareRuntime() error = nil, want cgroup update failure")
	}
}

func TestPrepareRuntimeFallsBackOnWritePermissionWhenPolicyAllowsIt(t *testing.T) {
	updateErr := errors.New("read-only cgroup")
	withCgroupRuntimeHooks(t, updateErr, true)
	request := &apipb.CreateContainerRequest{
		Resource: &apipb.LinuxContainerResources{MemoryLimitInBytes: 1024},
	}

	prep, err := PrepareRuntime(request, contract.HandlerOptions{CgroupPath: "/sandbox/test"}, RuntimePolicy{
		AllowWritePermissionFallback: true,
	})
	if err != nil {
		t.Fatalf("PrepareRuntime() error = %v", err)
	}
	if !prep.WritePermissionFallback {
		t.Fatalf("PrepareRuntime().WritePermissionFallback = false, want true")
	}
	if !errors.Is(prep.WritePermissionError, updateErr) {
		t.Fatalf("PrepareRuntime().WritePermissionError = %v, want %v", prep.WritePermissionError, updateErr)
	}
	if prep.Request == request {
		t.Fatalf("PrepareRuntime().Request = original request, want clone")
	}
	if prep.Request.Resource != nil {
		t.Fatalf("PrepareRuntime().Request.Resource = %#v, want nil", prep.Request.Resource)
	}
	if prep.Options.RuntimeCgroupPath != "/sandbox/test/workload" {
		t.Fatalf("PrepareRuntime().Options.RuntimeCgroupPath = %q, want workload path", prep.Options.RuntimeCgroupPath)
	}
}

func TestPrepareRuntimeUsesSanitizedResourceForBundleAndCgroupUpdate(t *testing.T) {
	withCgroupRuntimeHooks(t, nil, false)
	original := &apipb.LinuxContainerResources{MemoryLimitInBytes: 1024}
	sanitized := &apipb.LinuxContainerResources{MemoryLimitInBytes: 2048}
	request := &apipb.CreateContainerRequest{Resource: original}
	var updatedResource *apipb.LinuxContainerResources
	sanitizeResourceForDriver = func(_ os2.CgroupDriver, _ string, resource *apipb.LinuxContainerResources) *apipb.LinuxContainerResources {
		if resource != original {
			t.Fatalf("sanitizeResourceForDriver() resource = %#v, want original", resource)
		}
		return sanitized
	}
	updateCgroup = func(_ string, resource *apipb.LinuxContainerResources) error {
		updatedResource = resource
		return nil
	}

	prep, err := PrepareRuntime(request, contract.HandlerOptions{CgroupPath: "/sandbox/test"}, RuntimePolicy{})
	if err != nil {
		t.Fatalf("PrepareRuntime() error = %v", err)
	}
	if prep.Request == request {
		t.Fatalf("PrepareRuntime().Request = original request, want clone")
	}
	if prep.Request.Resource != sanitized {
		t.Fatalf("PrepareRuntime().Request.Resource = %#v, want sanitized", prep.Request.Resource)
	}
	if updatedResource != sanitized {
		t.Fatalf("updateCgroup() resource = %#v, want sanitized", updatedResource)
	}
	if request.Resource != original {
		t.Fatalf("PrepareRuntime() mutated original request resource")
	}
}
