package resources

import (
	"fmt"
	"strings"
	"sync"
)

type MockResourceManager struct {
	sync.Mutex
	resourceCount int
	usingCount    int
	maxSize       int
	maxCacheSize  int
	name          string
}

func (m *MockResourceManager) MaxSizeLimit() int {
	return m.maxSize
}

func (m *MockResourceManager) CacheSizeLimit() int {
	return m.maxCacheSize
}

func (m *MockResourceManager) UsingNum() int {
	return m.usingCount
}

type MockResource struct {
	id   string
	name string
}

func (m *MockResource) ToString() string {
	return fmt.Sprintf("%s@%s}", m.id, m.name)
}

func (m *MockResource) FromString(s string) error {
	if strings.Contains(s, "error") {
		return fmt.Errorf("error")
	}
	m.id = strings.Split(s, "@")[0]
	m.name = strings.Split(s, "@")[1]
	return nil
}

var _ Resource = &MockResource{}

func (m *MockResourceManager) Add(num int) int {
	m.resourceCount = m.resourceCount + num
	return num
}

func (m *MockResourceManager) Del(num int) {
	m.resourceCount = m.resourceCount - num
}

func (m *MockResourceManager) ResourceName() ResourceName {
	if m.name == "" {
		return ResourceName("mock")
	}
	return ResourceName(m.name)
}

func (m *MockResourceManager) CacheNum() int {
	return m.resourceCount
}

func (m *MockResourceManager) MaxSize() int {
	return m.maxSize
}

func (m *MockResourceManager) Allocate(opt AllocateOption) (Resource, error) {
	if m.resourceCount == 0 {
		return nil, fmt.Errorf("resource is empty")
	}
	if strings.Contains(opt.ContainerID, "error") {
		return nil, fmt.Errorf("error with %s", opt.ContainerID)
	}
	m.usingCount++
	m.resourceCount--
	return &MockResource{
		id:   opt.ContainerID,
		name: "mock-resource",
	}, nil
}

func (m *MockResourceManager) Recycle(id string) error {
	if strings.Contains(id, "error") {
		return fmt.Errorf("error with %s", id)
	}
	m.usingCount--
	m.resourceCount++
	return nil
}

func (m *MockResourceManager) Status() ([]string, []string) {
	return nil, nil
}

func (m *MockResourceManager) ShutDown() error {
	return nil
}

var (
	_ Manager   = &MockResourceManager{}
	_ resizable = &MockResourceManager{}
)
