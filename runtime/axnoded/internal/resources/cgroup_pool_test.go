//go:build linux

package resources

import (
	"fmt"
	"sync/atomic"
	"testing"

	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	cg "github.com/containerd/cgroups/v3/cgroup1"
	"github.com/containerd/cgroups/v3/cgroup1/stats"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
)

type stubCgroupDriver struct {
	createCalls int
	failFirst   int
}

func (d *stubCgroupDriver) Mode() string { return os2.CgroupModeV2 }

func (d *stubCgroupDriver) Create(group string, resources *spec.LinuxResources) (os2.Cgroup, error) {
	d.createCalls++
	if d.failFirst > 0 {
		d.failFirst--
		return nil, fmt.Errorf("create failed")
	}
	return &stubCgroup{}, nil
}

func (d *stubCgroupDriver) Load(group string) (os2.Cgroup, error) { return &stubCgroup{}, nil }

func (d *stubCgroupDriver) ExistingGroups(rootName string) ([]string, error) { return nil, nil }

func (d *stubCgroupDriver) Remove(group string) error { return nil }

func (d *stubCgroupDriver) LocalCPUCount() (int, error) { return 1, nil }

type stubCgroup struct{}

func (c *stubCgroup) Update(resources *spec.LinuxResources) error { return nil }

func (c *stubCgroup) Delete() error { return nil }

func (c *stubCgroup) Stats() (*os2.CgroupStats, error) { return &os2.CgroupStats{}, nil }

func (c *stubCgroup) AddProc(pid uint64) error { return nil }

func (c *stubCgroup) Processes(recursive bool) ([]int, error) { return nil, nil }

func TestCgroupManagerAllocateLazilyCreatesWhenPoolIsEmpty(t *testing.T) {
	driver := &stubCgroupDriver{}
	manager := &CgroupManager{
		size:                 2,
		cacheSize:            0,
		rootName:             "sandbox",
		usingID:              cmap.New[struct{}](),
		idleID:               queue.New(""),
		cgroups:              cmap.New[struct{}](),
		generator:            truncindex.NewFixLenGenerator(12, nil, truncindex.PrefixModifier("/sandbox/")),
		enableDestroyRecycle: false,
		storeMark:            atomic.Bool{},
		gcQueue:              queue.New(""),
		cgroupDriver:         driver,
	}

	resource, err := manager.Allocate(AllocateOption{ContainerID: "lazy-create"})
	assert.NoError(t, err)
	assert.NotEmpty(t, resource.ToString())
	assert.Equal(t, 1, driver.createCalls)
	assert.Equal(t, 1, manager.UsingNum())
}

func TestCgroupManagerAddReleasesIDAfterCreateFailure(t *testing.T) {
	driver := &stubCgroupDriver{failFirst: 1}
	manager := &CgroupManager{
		size:                 2,
		cacheSize:            0,
		rootName:             "sandbox",
		usingID:              cmap.New[struct{}](),
		idleID:               queue.New(""),
		cgroups:              cmap.New[struct{}](),
		generator:            truncindex.NewFixLenGenerator(12, nil, truncindex.PrefixModifier("/sandbox/")),
		enableDestroyRecycle: false,
		storeMark:            atomic.Bool{},
		gcQueue:              queue.New(""),
		cgroupDriver:         driver,
	}

	manager.Add(1)
	assert.Equal(t, 1, driver.createCalls)
	assert.Equal(t, 0, manager.cgroups.Count())

	resource, err := manager.Allocate(AllocateOption{ContainerID: "retry"})
	assert.NoError(t, err)
	assert.NotEmpty(t, resource.ToString())
	assert.Equal(t, 2, driver.createCalls)
	assert.Equal(t, 1, manager.UsingNum())
}

type MockCgroup struct {
	path string
}

func (m *MockCgroup) New(s string, resources *spec.LinuxResources) (cg.Cgroup, error) {
	return &MockCgroup{
		path: s,
	}, nil
}

func (m *MockCgroup) Add(process cg.Process, name ...cg.Name) error {
	return nil
}

func (m *MockCgroup) AddProc(u uint64, name ...cg.Name) error {
	return nil

}

func (m *MockCgroup) AddTask(process cg.Process, name ...cg.Name) error {
	return nil

}

func (m *MockCgroup) Delete() error {
	return nil

}

func (m *MockCgroup) MoveTo(cgroup cg.Cgroup) error {
	return nil

}

func (m *MockCgroup) Stat(handler ...cg.ErrorHandler) (*stats.Metrics, error) {
	return nil, nil

}

func (m *MockCgroup) Update(resources *spec.LinuxResources) error {
	return nil

}

func (m *MockCgroup) Processes(name cg.Name, b bool) ([]cg.Process, error) {
	return nil, nil
}

func (m *MockCgroup) Tasks(name cg.Name, b bool) ([]cg.Task, error) {
	return nil, nil
}

func (m *MockCgroup) Freeze() error {
	return nil

}

func (m *MockCgroup) Thaw() error {
	return nil

}

func (m *MockCgroup) OOMEventFD() (uintptr, error) {
	return 0, nil
}

func (m *MockCgroup) RegisterMemoryEvent(event cg.MemoryEvent) (uintptr, error) {
	return 0, nil
}

func (m *MockCgroup) State() cg.State {
	return cg.Unknown
}

func (m *MockCgroup) Subsystems() []cg.Subsystem {
	return []cg.Subsystem{}
}

var _ cg.Cgroup = &MockCgroup{}
