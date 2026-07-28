package nodeinventory

import (
	"fmt"
	"os"

	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type fakeContainerManager struct {
	list          []*container.Container
	pools         map[string]PoolInventory
	runtimeCgroup map[string]string
}

func (m *fakeContainerManager) List(...container.ListOption) []*container.Container {
	return m.list
}

func (m *fakeContainerManager) ResourcePoolStatus(name resources.ResourceName) (resources.PoolStatus, error) {
	pool, ok := m.pools[string(name)]
	if !ok {
		return resources.PoolStatus{}, fmt.Errorf("resource manager for %s not found", name)
	}
	return resources.PoolStatus{
		Using:       pool.Using,
		Idle:        pool.Idle,
		Capacity:    pool.Capacity,
		Unavailable: pool.Unavailable,
	}, nil
}

func (m *fakeContainerManager) RuntimeCgroupPath(id string) (string, error) {
	if path, ok := m.runtimeCgroup[id]; ok {
		return path, nil
	}
	return "", os.ErrNotExist
}

type fakeStatusStorage struct {
	status container.Status
}

func (s *fakeStatusStorage) Get() container.Status {
	return s.status
}

func (s *fakeStatusStorage) UpdateSync(update container.UpdateFunc) error {
	next, err := update(s.status)
	if err != nil {
		return err
	}
	s.status = next
	return nil
}

func (s *fakeStatusStorage) Update(update container.UpdateFunc) error {
	return s.UpdateSync(update)
}

func (s *fakeStatusStorage) Delete() error {
	s.status = container.Status{}
	return nil
}

type fakeLangRuntimeManager struct {
	lrt   []*langruntime.LanguageRuntime
	byID  map[string]*langruntime.LanguageRuntime
	stats langruntime.RetentionStats
}

func (m *fakeLangRuntimeManager) GetLangRuntime(id string) *langruntime.LanguageRuntime {
	return m.byID[id]
}

func (m *fakeLangRuntimeManager) List() []*langruntime.LanguageRuntime {
	return m.lrt
}

func (m *fakeLangRuntimeManager) RetentionStats() langruntime.RetentionStats {
	return m.stats
}

type fakeCgroup struct {
	stats *os2.CgroupStats
}

func (c *fakeCgroup) Update(*specs.LinuxResources) error { return nil }
func (c *fakeCgroup) Delete() error                      { return nil }
func (c *fakeCgroup) Stats() (*os2.CgroupStats, error)   { return c.stats, nil }
func (c *fakeCgroup) AddProc(uint64) error               { return nil }
func (c *fakeCgroup) Processes(bool) ([]int, error)      { return nil, nil }

type fakeCgroupDriver struct {
	stats map[string]*os2.CgroupStats
}

func (d *fakeCgroupDriver) Mode() string { return os2.CgroupModeV2 }
func (d *fakeCgroupDriver) Create(string, *specs.LinuxResources) (os2.Cgroup, error) {
	return nil, nil
}
func (d *fakeCgroupDriver) Load(group string) (os2.Cgroup, error) {
	return &fakeCgroup{stats: d.stats[group]}, nil
}
func (d *fakeCgroupDriver) ExistingGroups(string) ([]string, error) { return nil, nil }
func (d *fakeCgroupDriver) Remove(string) error                     { return nil }
func (d *fakeCgroupDriver) LocalCPUCount() (int, error)             { return 1, nil }
