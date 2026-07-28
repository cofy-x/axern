package resources

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

type InterfaceManager struct {
	size      int
	cacheSize int
	IpRange   string
	BridgeIp  net.IP
	idleIp    *queue.Queue[string]

	poolController *poolController

	allInterfaces   []net.Interface
	interfaces      *queue.Queue[string]
	usingInterfaces cmap.ConcurrentMap[string, struct{}]

	bridgeLink netlink.Link

	mask net.IPMask

	slotInit         sync.Once
	activeSlots      atomic.Int64
	quarantinedSlots atomic.Int64
	buildMu          sync.Mutex
	buildingSlots    int
	buildChanged     chan struct{}

	// store resource string.
	db stateStore

	destroyDeviceFunc               func(net.Interface) error
	createDeviceFunc                func(string) error
	lookupDeviceFunc                func(string) (*net.Interface, error)
	validateDeviceFunc              func(*NetResource) error
	validateDeviceConfigurationFunc func(*NetResource) error
	deleteNeighborFunc              func(net.IP) error

	// storeMark is used to mark whether the cgroup id need to be stored.
	// If it's true, manager should not exit.
	storeMark atomic.Bool
	storeStop chan struct{}
	storeDone chan struct{}
	storeOnce sync.Once
}

func (m *InterfaceManager) MaxSizeLimit() int {
	if m.size == 0 {
		return config.DefaultMaxContainerNum
	}
	return m.size
}

func (m *InterfaceManager) CacheSizeLimit() int {
	if m.cacheSize == 0 {
		return config.DefaultMaxCacheLimitNum
	}
	return m.cacheSize
}

func (m *InterfaceManager) UsingNum() int {
	return m.usingInterfaces.Count()
}

func (m *InterfaceManager) UnavailableNum() int {
	return int(m.quarantinedSlots.Load())
}

func (m *InterfaceManager) initializeSlots() {
	m.slotInit.Do(func() {
		m.activeSlots.Store(int64(m.interfaces.Length() + m.usingInterfaces.Count()))
	})
}

func (m *InterfaceManager) reserveSlot() bool {
	m.initializeSlots()
	limit := int64(m.MaxSizeLimit())
	for {
		active := m.activeSlots.Load()
		if active >= limit {
			return false
		}
		if m.activeSlots.CompareAndSwap(active, active+1) {
			return true
		}
	}
}

func (m *InterfaceManager) releaseSlot() {
	m.initializeSlots()
	for {
		active := m.activeSlots.Load()
		if active <= 0 {
			logrus.Error("interface resource slot accounting underflow")
			return
		}
		if m.activeSlots.CompareAndSwap(active, active-1) {
			return
		}
	}
}

func (m *InterfaceManager) beginBuild() {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	m.buildingSlots++
	if m.buildChanged == nil {
		m.buildChanged = make(chan struct{})
	}
}

func (m *InterfaceManager) endBuild() {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	if m.buildingSlots <= 0 || m.buildChanged == nil {
		logrus.Error("interface resource build accounting underflow")
		return
	}
	m.buildingSlots--
	close(m.buildChanged)
	m.buildChanged = make(chan struct{})
}

func (m *InterfaceManager) waitForBuild(ctx context.Context) error {
	m.buildMu.Lock()
	if m.buildingSlots == 0 {
		m.buildMu.Unlock()
		return errord.ErrResourceExhausted
	}
	changed := m.buildChanged
	m.buildMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-changed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *InterfaceManager) ShutDown() error {
	m.stopStoreLoop()
	if m.storeMark.Load() {
		m.store()
	}
	m.cleanup()
	return nil
}

func (m *InterfaceManager) ResourceName() ResourceName {
	return InterfaceResourceName
}

func (m *InterfaceManager) CacheNum() int {
	return m.interfaces.Length()
}

func (m *InterfaceManager) MaxSize() int {
	return m.size
}

func (m *InterfaceManager) setPoolController(controller *poolController) {
	m.poolController = controller
	recordPoolState(m)
}

func (m *InterfaceManager) requestPoolRefill(trigger string) {
	if m.poolController != nil {
		m.poolController.request(trigger)
	}
}

func (m *InterfaceManager) destroyInterface(dev net.Interface) error {
	var err error
	if m.destroyDeviceFunc != nil {
		err = m.destroyDeviceFunc(dev)
	} else {
		err = m.destroyDevice(dev)
	}
	if isLinkAbsent(err) {
		return nil
	}
	return err
}

func (m *InterfaceManager) createInterfaceDevice(ip string) error {
	if m.createDeviceFunc != nil {
		return m.createDeviceFunc(ip)
	}
	return m.createDevice(ip)
}

func (m *InterfaceManager) lookupInterface(name string) (*net.Interface, error) {
	if m.lookupDeviceFunc != nil {
		return m.lookupDeviceFunc(name)
	}
	return net.InterfaceByName(name)
}

func (m *InterfaceManager) validateInterface(dev *NetResource) error {
	if m.validateDeviceFunc != nil {
		return m.validateDeviceFunc(dev)
	}
	return m.validateDevice(dev)
}

func (m *InterfaceManager) validateInterfaceConfiguration(dev *NetResource) error {
	if m.validateDeviceConfigurationFunc != nil {
		return m.validateDeviceConfigurationFunc(dev)
	}
	return m.validateDeviceConfiguration(dev)
}

var _ Manager = &InterfaceManager{}
