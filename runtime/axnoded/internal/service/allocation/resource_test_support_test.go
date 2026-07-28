package allocation

import (
	"path/filepath"
	"sync"

	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
)

func newTestResourceManagers() []resourcemanager.Manager {
	return []resourcemanager.Manager{
		newTestResourceManager(resourcemanager.CgroupResourceName, "/sandbox-test"),
	}
}

type testResourceManager struct {
	name   resourcemanager.ResourceName
	prefix string
	mu     sync.Mutex
	using  map[string]struct{}
}

func newTestResourceManager(name resourcemanager.ResourceName, prefix string) *testResourceManager {
	return &testResourceManager{
		name:   name,
		prefix: prefix,
		using:  make(map[string]struct{}),
	}
}

func (m *testResourceManager) Allocate(opt resourcemanager.AllocateOption) (resourcemanager.Resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := opt.ContainerID
	if id == "" {
		id = "test-resource"
	}
	value := filepath.Join(m.prefix, id)
	m.using[value] = struct{}{}
	return resourcemanager.NewStringResource(value), nil
}

func (m *testResourceManager) Recycle(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.using, id)
	return nil
}

func (m *testResourceManager) Status() ([]string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	using := make([]string, 0, len(m.using))
	for id := range m.using {
		using = append(using, id)
	}
	return using, nil
}

func (m *testResourceManager) ShutDown() error {
	return nil
}

func (m *testResourceManager) ResourceName() resourcemanager.ResourceName {
	return m.name
}
