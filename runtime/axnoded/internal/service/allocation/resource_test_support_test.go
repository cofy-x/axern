package allocation

import (
	"fmt"
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
	name        resourcemanager.ResourceName
	prefix      string
	mu          sync.Mutex
	using       map[string]struct{}
	owners      map[string]string
	allocateErr error
}

func newTestResourceManager(name resourcemanager.ResourceName, prefix string) *testResourceManager {
	return &testResourceManager{
		name:   name,
		prefix: prefix,
		using:  make(map[string]struct{}),
		owners: make(map[string]string),
	}
}

func (m *testResourceManager) Allocate(opt resourcemanager.AllocateOption) (resourcemanager.Resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.allocateErr != nil {
		return resourcemanager.EmptyStringResource, m.allocateErr
	}
	id := opt.ContainerID
	if id == "" {
		id = "test-resource"
	}
	value := filepath.Join(m.prefix, id)
	m.using[value] = struct{}{}
	m.owners[id] = value
	return resourcemanager.NewStringResource(value), nil
}

func (m *testResourceManager) AllocationResource(id string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.owners[id]
	return value, ok
}

func newRejectingTestResourceManager(name resourcemanager.ResourceName) *testResourceManager {
	manager := newTestResourceManager(name, "/rejected")
	manager.allocateErr = fmt.Errorf("node-local admission rejected")
	return manager
}

func (m *testResourceManager) Recycle(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.using, id)
	for owner, value := range m.owners {
		if value == id {
			delete(m.owners, owner)
		}
	}
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
