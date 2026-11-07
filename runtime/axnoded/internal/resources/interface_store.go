package resources

import (
	"fmt"
	"net"
	"sort"
	"strings"
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

func LoadNetworkManager(db stateStore, size, cacheSize int, cfg config.NetworkConfig) (NetworkManager, error) {
	switch cfg.NatBackend {
	case config.NatBackendIptables, config.NatBackendEBPF:
		return NewInterfaceManager(db, cfg.IPRange, size, cacheSize, cfg.NatBackend)
	}
	return nil, fmt.Errorf("unsupported nat_backend: %s", cfg.NatBackend)
}

func NewInterfaceManager(db stateStore, ipRange string, size int, cacheSize int, natBackend string) (*InterfaceManager, error) {
	var ledger apipb.NetworkLedger
	err := db.LoadSnapshot(config.BridgeIPBucket, &ledger)
	if err != nil && !errord.IsNotFound(err) {
		return nil, err
	}
	if err == nil {
		logrus.Infof("load network interface lease num: %v", len(ledger.Leases))
	}

	if size > maxVethNum {
		size = maxVethNum
	}
	gatewayIp, mask, ips, err := generateIP(ipRange, uint32(size))
	if err != nil {
		return nil, err
	}

	usingInterfaces := cmap.New[struct{}]()
	allocationLeases := cmap.New[string]()
	interfaceOwners := make(map[string]string, len(ledger.Leases))
	ipOwners := make(map[string]string, len(ledger.Leases))
	for _, lease := range ledger.Leases {
		allocationID := strings.TrimSpace(lease.GetAllocationID())
		interfaceName := strings.TrimSpace(lease.GetHostInterfaceName())
		ip := net.ParseIP(strings.TrimSpace(lease.GetIp()))
		netnsPath := strings.TrimSpace(lease.GetNetnsPath())
		if allocationID == "" || interfaceName == "" || ip == nil || netnsPath == "" {
			return nil, fmt.Errorf("network ledger contains an incomplete lease")
		}
		canonicalIP := ip.String()
		if allocationLeases.Has(allocationID) || interfaceOwners[interfaceName] != "" || ipOwners[canonicalIP] != "" {
			return nil, fmt.Errorf("network ledger contains duplicate ownership")
		}
		resource := (&NetResource{
			Interface: &net.Interface{Name: interfaceName},
			Ip:        ip,
			Mask:      mask,
			Gateway:   gatewayIp,
			Type:      "bridge",
			NetNSPath: netnsPath,
		}).ToString()
		allocationLeases.Set(allocationID, resource)
		usingInterfaces.Set(resource, struct{}{})
		interfaceOwners[interfaceName] = allocationID
		ipOwners[canonicalIP] = allocationID
	}
	for item := range allocationLeases.IterBuffered() {
		resource, err := NewNetResource(item.Val)
		if err != nil {
			return nil, fmt.Errorf("decode network lease for allocation %s: %w", item.Key, err)
		}
		if err := validateNetworkLeaseBinding(resource, ipRange, gatewayIp); err != nil {
			return nil, fmt.Errorf("validate network lease for allocation %s: %w", item.Key, err)
		}
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
		db:               db,
		cacheSize:        cacheSize,
		idleIp:           queue.New(""),
		size:             size,
		IpRange:          ipRange,
		BridgeIp:         gatewayIp,
		interfaces:       queue.New(""),
		usingInterfaces:  usingInterfaces,
		allocationLeases: &allocationLeases,
		bridgeLink:       bridgeLink,
		mask:             mask,
	}

	if err = manager.load(ips); err != nil {
		return nil, err
	}
	manager.initializeSlots()
	return manager, nil
}

func (m *InterfaceManager) storeLeasesLocked() error {
	m.ensureLeaseIndexLocked()
	start := time.Now()
	defer func() {
		logrus.Debugf("store network interface %v using id cost: %v ms", m.usingInterfaces.Count(), time.Since(start).Milliseconds())
	}()
	owners := m.allocationLeases.Keys()
	sort.Strings(owners)
	leases := make([]*apipb.NetworkLease, 0, len(owners))
	for _, owner := range owners {
		resource, ok := m.allocationLeases.Get(owner)
		if !ok {
			continue
		}
		network, err := NewNetResource(resource)
		if err != nil || network.Interface == nil || strings.TrimSpace(network.Interface.Name) == "" || network.Ip == nil || strings.TrimSpace(network.NetNSPath) == "" {
			return fmt.Errorf("network lease for allocation %s is invalid", owner)
		}
		leases = append(leases, &apipb.NetworkLease{
			AllocationID:      owner,
			HostInterfaceName: network.Interface.Name,
			Ip:                network.Ip.String(),
			NetnsPath:         network.NetNSPath,
		})
	}
	dataToStore := &apipb.NetworkLedger{Leases: leases}
	if m.db == nil {
		return nil
	}
	if err := m.db.SaveSnapshot(config.BridgeIPBucket, dataToStore); err != nil {
		return fmt.Errorf("store network interface leases: %w", err)
	}
	return nil
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
	m.updateInterfacesCache()
	return m.loadInterfaces(ips, m.allInterfaces)
}

func (m *InterfaceManager) loadInterfaces(ips map[string]struct{}, devs []net.Interface) error {
	_, addressNet, err := net.ParseCIDR(m.IpRange)
	if err != nil {
		return err
	}

	ownerByName := make(map[string]string)
	for item := range m.allocationLeases.IterBuffered() {
		resource, err := NewNetResource(item.Val)
		if err != nil || resource.Interface == nil || resource.Interface.Name == "" || resource.Ip == nil || resource.NetNSPath == "" {
			return fmt.Errorf("network lease for allocation %s is invalid", item.Key)
		}
		if err := validateNetworkLeaseBinding(resource, m.IpRange, m.BridgeIp); err != nil {
			return fmt.Errorf("validate network lease for allocation %s: %w", item.Key, err)
		}
		if _, exists := ownerByName[resource.Interface.Name]; exists {
			return fmt.Errorf("network ledger assigns interface %s more than once", resource.Interface.Name)
		}
		ownerByName[resource.Interface.Name] = item.Key
		delete(ips, resource.Ip.String())
	}
	foundOwners := make(map[string]struct{}, len(ownerByName))

	for idx := range devs {
		if strings.HasPrefix(devs[idx].Name, config.HostVethPrefix) {
			if owner, assigned := ownerByName[devs[idx].Name]; assigned {
				// A live runsc netstack has already consumed the namespace's
				// addresses. Preserve its binding without changing the link or
				// applying idle-pool L3 checks. Complete runtime recovery audits
				// the exact Allocation, namespace and peer before readiness.
				foundOwners[owner] = struct{}{}
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
			if err := m.validateInterfaceConfiguration(dev); err != nil {
				// Missing preparation may instead mean that runsc consumed L3
				// while its ownership record was lost. Neither a name prefix nor
				// ledger absence authorizes rebuilding a possibly live interface.
				return fmt.Errorf("validate unassigned interface %s before pool recovery: %w", dev.Interface.Name, err)
			}
			m.resetBridgeNeighbor(dev.Ip, "load")
			m.interfaces.Push(dev.ToString())
			delete(ips, ip.String())
		}
	}

	for item := range m.allocationLeases.IterBuffered() {
		if _, found := foundOwners[item.Key]; found {
			continue
		}
		logrus.Errorf("assigned network interface for allocation %s is absent; preserving ownership until runtime inventory reconciliation", item.Key)
	}

	for ip := range ips {
		if addressNet.Contains(net.ParseIP(ip)) {
			m.idleIp.Push(ip)
		}
	}

	logrus.Debugf("load network interface idle num: %v, using num: %v, idle ip: %v", m.interfaces.Length(), m.usingInterfaces.Count(), m.idleIp.Length())
	return nil
}
