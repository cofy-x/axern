package resources

import (
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	_ "github.com/cofy-x/axern/runtime/axnoded/internal/network/bridge"
	_ "github.com/cofy-x/axern/runtime/axnoded/internal/network/ebpf"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

func LoadNetworkManager(db stateStore, size, cacheSize int, cfg config.NetworkConfig) (Manager, error) {
	switch cfg.NatBackend {
	case config.NatBackendIptables, config.NatBackendEBPF:
		return NewInterfaceManager(db, cfg.IPRange, size, cacheSize, cfg.NatBackend)
	}
	return nil, fmt.Errorf("unsupported nat_backend: %s", cfg.NatBackend)
}

func NewInterfaceManager(db stateStore, ipRange string, size int, cacheSize int, natBackend string) (*InterfaceManager, error) {
	// load using id from db
	var usingID apipb.Slice
	err := db.LoadSnapshot(config.BridgeIPBucket, &usingID)
	if err != nil && !errord.IsNotFound(err) {
		return nil, err
	}
	if err == nil {
		logrus.Infof("load network interface using id num: %v", len(usingID.Items))
	}

	usingInterfaces := cmap.New[struct{}]()
	for idx := range usingID.Items {
		usingInterfaces.Set(usingID.Items[idx], struct{}{})
	}

	if size > maxVethNum {
		size = maxVethNum
	}
	gatewayIp, mask, ips, err := generateIP(ipRange, uint32(size))
	if err != nil {
		return nil, err
	}

	if err := initBridge(ipRange, natBackend); err != nil {
		if cleanErr := cleanBridge(natBackend, ipRange); cleanErr != nil {
			logrus.Warnf("clean bridge after init failed: %v", cleanErr)
		}
		return nil, err
	}

	bridgeLink, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return nil, err
	}

	cacheSize = calcluteCacheSize(cacheSize)

	manager := &InterfaceManager{
		db:              db,
		cacheSize:       cacheSize,
		idleIp:          queue.New(""),
		size:            size,
		IpRange:         ipRange,
		BridgeIp:        gatewayIp,
		interfaces:      queue.New(""),
		usingInterfaces: usingInterfaces,
		bridgeLink:      bridgeLink,
		mask:            mask,
		storeMark:       atomic.Bool{},
		storeStop:       make(chan struct{}),
		storeDone:       make(chan struct{}),
	}

	if err = manager.load(ips); err != nil {
		return nil, err
	}
	manager.initializeSlots()
	manager.keepStoring()
	return manager, nil
}

func (m *InterfaceManager) keepStoring() {
	go func() {
		defer close(m.storeDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if m.storeMark.Load() {
					m.storeMark.Store(false)
					m.store()
				}
			case <-m.storeStop:
				return
			}
		}
	}()
}

func (m *InterfaceManager) stopStoreLoop() {
	if m.storeStop == nil || m.storeDone == nil {
		return
	}
	m.storeOnce.Do(func() { close(m.storeStop) })
	<-m.storeDone
}

func (m *InterfaceManager) store() {
	start := time.Now()
	defer func() {
		logrus.Debugf("store network interface %v using id cost: %v ms", m.usingInterfaces.Count(), time.Since(start).Milliseconds())
	}()
	dm := m.usingInterfaces.Keys()
	dmToStr := make([]string, 0, len(dm))
	for idx := range dm {
		dmToStr = append(dmToStr, dm[idx])
	}
	dataToStore := &apipb.Slice{Items: dmToStr}
	if err := m.db.SaveSnapshot(config.BridgeIPBucket, dataToStore); err != nil {
		logrus.Warnf("store network interface using id failed: %v", err)
	}
}

// Call it when received SIGTERM sent by pod destroying
// We can't count on auto deleting when netns deleting because we should slow down the deleting
func (m *InterfaceManager) cleanup() {
	m.initializeSlots()
	logrus.Debugf("start to cleanup interfaces")

	interfaces := m.interfaces.List()
	for _, devStr := range interfaces {
		if devStr == "" {
			logrus.Errorf("no idle interface")
			continue
		}
		dev, err := NewNetResource(devStr)
		if err != nil {
			logrus.Errorf("parse net resource failed: %v", err)
			continue
		}
		if dev.Interface == nil {
			logrus.Errorf("destory interface %s failed: interface missing", dev.ToString())
			continue
		}
		if err := m.destroyInterface(*dev.Interface); err != nil {
			logrus.Errorf("destory interface %s failed: %v", dev.ToString(), err)
			continue
		}
		m.releaseSlot()

		// Slow down the deletion to reduce performance impact to host
		time.Sleep(20 * time.Millisecond)
	}

	logrus.Debugf("finish to cleanup interfaces")
}

func (m *InterfaceManager) load(ips map[string]struct{}) error {
	_, addressNet, err := net.ParseCIDR(m.IpRange)
	if err != nil {
		return err
	}

	m.updateInterfacesCache()

	devs := m.allInterfaces
	for idx := range devs {
		if strings.HasPrefix(devs[idx].Name, config.HostVethPrefix) {
			// set host veth up
			link, err := netlink.LinkByName(devs[idx].Name)
			if err != nil {
				logrus.Errorf("get link by name %v failed: %v", devs[idx].Name, err)
				continue
			}
			if err := netlink.LinkSetUp(link); err != nil {
				logrus.Errorf("set link %v up failed: %v", devs[idx].Name, err)
				continue
			}
			ip := vethToIP(devs[idx].Name, m.IpRange)
			if ip == nil {
				logrus.Warnf("ignore interface %s whose address cannot be reconstructed from %s", devs[idx].Name, m.IpRange)
				continue
			}
			dev := &NetResource{
				Interface: &devs[idx],
				Ip:        ip,
				Mask:      m.mask,
				Gateway:   m.BridgeIp,
				Type:      "bridge",
				NetNSPath: netnsPath(ip.String()),
			}
			if !m.usingInterfaces.Has(dev.ToString()) {
				if err := m.validateInterfaceConfiguration(dev); err != nil {
					logrus.Warnf("recovered idle interface %s is stale, rebuilding: %v", dev.ToString(), err)
					rebuilt, rebuildErr := m.rebuildDevice(dev)
					if rebuildErr != nil {
						logrus.Errorf("rebuild recovered interface %s failed: %v", dev.ToString(), rebuildErr)
						m.idleIp.Push(ip.String())
						delete(ips, ip.String())
						continue
					}
					dev = rebuilt
				}
				m.resetBridgeNeighbor(dev.Ip, "load")
				m.interfaces.Push(dev.ToString())
			}
			delete(ips, ip.String())
		}
	}

	for ip := range ips {
		if addressNet.Contains(net.ParseIP(ip)) {
			m.idleIp.Push(ip)
		}
	}

	logrus.Debugf("load network interface idle num: %v, using num: %v, idle ip: %v", m.interfaces.Length(), m.usingInterfaces.Count(), m.idleIp.Length())
	return nil
}
