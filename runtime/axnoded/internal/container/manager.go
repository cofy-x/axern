package container

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/sirupsen/logrus"
)

type Manager struct {
	// sandbox container root
	root        string
	recyclePath string

	containers     cmap.ConcurrentMap[string, *Container]
	serviceHandler cmap.ConcurrentMap[string, contract.RuntimeHandler]
	// resourceManagers is a map of resource manager, key is resource type
	resourceManagers cmap.ConcurrentMap[string, resourcemanager.Manager]

	monitorStopChan cmap.ConcurrentMap[string, chan struct{}]
	// handle container event asynchronously, largest 200 events
	syncEventChan chan Event
	// check id is valid
	idGenerator truncindex.UniqueIdGenerator

	stopChan chan struct{}
	stopOnce sync.Once

	healthChan   chan bool
	exitObserver func(Event)

	isHousekeepingRunning atomic.Bool
}

func NewManager(root string, handlers cmap.ConcurrentMap[string, contract.RuntimeHandler], healthChan chan bool, managers ...resourcemanager.Manager) (*Manager, error) {
	if err := Os().MkdirAll(filepath.Join(root, config.RecycleBin), 0755); err != nil {
		return nil, err
	}

	m := &Manager{
		root:             filepath.Join(root, "containers"),
		recyclePath:      filepath.Join(root, config.RecycleBin),
		containers:       cmap.New[*Container](),
		serviceHandler:   handlers,
		monitorStopChan:  cmap.New[chan struct{}](),
		resourceManagers: cmap.New[resourcemanager.Manager](),
		idGenerator:      truncindex.NewTruncGenerator(config.SandboxContainerPrefix, []string{}),
		syncEventChan:    make(chan Event, 4096),
		stopChan:         make(chan struct{}),
		healthChan:       healthChan,
	}

	for _, manager := range managers {
		m.resourceManagers.Set(string(manager.ResourceName()), manager)
	}

	if err := m.loadContainers(); err != nil {
		return nil, err
	}
	if err := m.reconcileResourceClaims(); err != nil {
		return nil, err
	}

	for item := range m.containers.IterBuffered() {
		if item.Val != nil && item.Val.Metadata != nil {
			m.startMonitorGoroutine(item.Val.Metadata, make(chan struct{}))
		}
	}
	if count := m.containers.Count(); count > 0 {
		logrus.Infof("recovered %d containers from disk, monitors started", count)
	}

	return m, nil
}

func (m *Manager) SetExitObserver(observer func(Event)) {
	if m == nil {
		return
	}
	m.exitObserver = observer
}

func (m *Manager) Handlers() []contract.RuntimeHandler {
	handlers := make([]contract.RuntimeHandler, 0, m.serviceHandler.Count())
	for item := range m.serviceHandler.IterBuffered() {
		handlers = append(handlers, item.Val)
	}
	return handlers
}

func (m *Manager) Get(id string) (*Container, error) {
	if c, ok := m.containers.Get(id); ok {
		return c, nil
	}
	return nil, errord.ErrNotFound
}

func (m *Manager) SetResources(id string, resources *runtimeapi.LinuxContainerResources, spec *commonv1.ResourceSpec) error {
	c, ok := m.containers.Get(id)
	if !ok || c == nil || c.Status == nil {
		return errord.ErrNotFound
	}
	return c.Status.UpdateSync(func(status Status) (Status, error) {
		copy := deepCopyOf(Status{LinuxResources: resources, ResourceSpec: spec})
		status.LinuxResources = copy.LinuxResources
		status.ResourceSpec = copy.ResourceSpec
		return status, nil
	})
}

func (m *Manager) UpdateLabels(id string, labels map[string]string) error {
	c, ok := m.containers.Get(id)
	if !ok {
		return errord.ErrNotFound
	}
	if len(labels) == 0 {
		return nil
	}
	needUpdate := false
	for k, v := range labels {
		if c.Metadata.Labels == nil {
			c.Metadata.Labels = make(map[string]string)
		}
		if c.Metadata.Labels[k] != v {
			c.Metadata.Labels[k] = v
			needUpdate = true
		}
	}
	if !needUpdate {
		return nil
	}
	m.StoreMetadata(id, c.Metadata)
	return nil
}

func (m *Manager) List(option ...ListOption) []*Container {
	var containers []*Container
	for _, ct := range m.containers.Items() {
		satisfy := true
		for _, opt := range option {
			if !opt(ct) {
				satisfy = false
				break
			}
		}
		if satisfy {
			containers = append(containers, ct)
		}
	}
	return containers
}

type ListOption func(*Container) bool

func ListFilterByState(state runtimeapi.ContainerState) ListOption {
	return func(c *Container) bool {
		return c.Status.Get().State() == state
	}
}

func ListFilterById(id string) ListOption {
	return func(c *Container) bool {
		if c == nil || c.Metadata == nil {
			logrus.Errorf("ListFilterByID: Got invalid container %+v", c)
			return false
		}
		return c.Metadata.ID == id
	}
}

func ListFilterByLabels(labels map[string]string) ListOption {
	return func(c *Container) bool {
		if c == nil || c.Metadata == nil {
			logrus.Errorf("ListFilterByLabels: Got invalid container %+v", c)
			return false
		}

		if len(labels) == 0 {
			return true
		}

		if c.Metadata.Labels == nil {
			return false
		}

		for k, v := range labels {
			if c.Metadata.Labels[k] != v && v != "" {
				return false
			}
		}
		return true
	}
}

func (m *Manager) Resources(kind string) ([]string, error) {
	manager, ok := m.resourceManagers.Get(kind)
	if !ok {
		return nil, fmt.Errorf("resource manager for %s not found", kind)
	}
	using, idle := manager.Status()
	return append(using, idle...), nil
}

type poolSizer interface {
	MaxSizeLimit() int
}

type poolUnavailabilityReporter interface {
	UnavailableNum() int
}

func (m *Manager) ResourcePoolStatus(kind resourcemanager.ResourceName) (resourcemanager.PoolStatus, error) {
	manager, ok := m.resourceManagers.Get(string(kind))
	if !ok {
		return resourcemanager.PoolStatus{}, fmt.Errorf("resource manager for %s not found", kind)
	}
	usingIDs, idleIDs := manager.Status()
	status := resourcemanager.PoolStatus{
		Using:    len(usingIDs),
		Idle:     len(idleIDs),
		Capacity: len(usingIDs) + len(idleIDs),
	}
	if sizer, ok := manager.(poolSizer); ok {
		status.Capacity = sizer.MaxSizeLimit()
	}
	if reporter, ok := manager.(poolUnavailabilityReporter); ok {
		status.Unavailable = reporter.UnavailableNum()
	}
	return status, nil
}
