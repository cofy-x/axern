package resources

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/sirupsen/logrus"
)

// Add will get ip from m.idleIp and add veth pair into m.interfaces
func (m *InterfaceManager) Add(num int) int {
	m.initializeSlots()
	logrus.Debugf("start to add %d interfaces", num)
	if m.idleIp.Num() == 0 {
		logrus.Errorf("no idle ip, skip add")
		recordPoolState(m)
		return 0
	}

	created := 0
	for i := 0; i < num; i++ {
		err := m.buildCachedResource()
		if err != nil {
			if errors.Is(err, errord.ErrResourceExhausted) {
				logrus.Debug("interface pool reached node capacity")
				break
			}
			logrus.Errorf("add veth failed: %v", err)
			continue
		}
		created++

		// Slow down the creating to reduce performance impact to host
		if i+1 < num {
			time.Sleep(20 * time.Millisecond)
		}
	}

	if created == 0 {
		logrus.Debugf("no interfaces created (requested %d)", num)
		recordPoolState(m)
		return 0
	}

	recordPoolState(m)
	logrus.Debugf("finish to add %d interfaces", num)
	return created
}

func (m *InterfaceManager) buildCachedResource() error {
	m.beginBuild()
	defer m.endBuild()

	netResource, err := m.buildOneResource()
	if err != nil {
		return err
	}
	m.interfaces.Push(netResource.ToString())
	return nil
}

// Del will delete veth pair from m.interfaces and add ip into m.idleIp
func (m *InterfaceManager) Del(num int) {
	m.initializeSlots()
	logrus.Debugf("start to delete %d interfaces", num)
	wg := sync.WaitGroup{}
	for i := 0; i < num; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			devStr := m.interfaces.Pop()
			if devStr == "" {
				logrus.Errorf("no idle interface")
				return
			}
			dev, err := NewNetResource(devStr)
			if err != nil {
				logrus.Errorf("parse net resource failed: %v", err)
				m.releaseSlot()
				return
			}
			if dev.Interface == nil {
				logrus.Errorf("destory interface %s failed: interface missing", dev.ToString())
				if dev.Ip != nil {
					m.idleIp.Push(dev.Ip.String())
				}
				m.releaseSlot()
				return
			}
			if err := m.destroyInterface(*dev.Interface); err != nil {
				logrus.Errorf("destory interface %s failed: %v", dev.ToString(), err)
				m.interfaces.Push(devStr)
				return
			} else {
				logrus.Infof("deleted interface: %s ", dev.Interface.Name)
			}
			m.idleIp.Push(dev.Ip.String())
			m.releaseSlot()
		}()
	}
	wg.Wait()
	recordPoolState(m)
	logrus.Debugf("finish to delete %d interfaces", num)
}

func (m *InterfaceManager) Allocate(opt AllocateOption) (Resource, error) {
	m.initializeSlots()
	for {
		cacheStarted := time.Now()
		if netResourceStr := m.interfaces.Pop(); netResourceStr != "" {
			metrics.RecordResourceAllocateStage(string(InterfaceResourceName), "cache_pop", "hit", time.Since(cacheStarted).Seconds())
			return m.allocateCachedInterface(netResourceStr)
		}
		metrics.RecordResourceAllocateStage(string(InterfaceResourceName), "cache_pop", "miss", time.Since(cacheStarted).Seconds())

		m.requestPoolRefill(ResourcePoolTriggerAllocationMiss)
		createStarted := time.Now()
		netResource, err := m.buildOneResource()
		createResult := "ok"
		if err != nil {
			createResult = "error"
		}
		metrics.RecordResourceAllocateStage(string(InterfaceResourceName), "sync_create", createResult, time.Since(createStarted).Seconds())
		if err == nil {
			m.markInterfaceUsing(netResource)
			metrics.RecordResourcePoolAllocate(string(InterfaceResourceName), ResourcePoolAllocateMissSyncCreate)
			m.requestPoolRefill(ResourcePoolTriggerAllocationMiss)
			return netResource, nil
		}
		if errors.Is(err, errord.ErrResourceExhausted) {
			waitStarted := time.Now()
			waitErr := m.waitForBuild(opt.Context)
			waitResult := "ok"
			if waitErr != nil {
				waitResult = "error"
			}
			metrics.RecordResourceAllocateStage(string(InterfaceResourceName), "wait_for_refill", waitResult, time.Since(waitStarted).Seconds())
			if waitErr == nil {
				continue
			} else if !errors.Is(waitErr, errord.ErrResourceExhausted) {
				return EmptyStringResource, waitErr
			}
		}

		result := ResourcePoolAllocateError
		if errors.Is(err, errord.ErrResourceExhausted) {
			result = ResourcePoolAllocateExhausted
		}
		metrics.RecordResourcePoolAllocate(string(InterfaceResourceName), result)
		recordPoolState(m)
		return EmptyStringResource, err
	}
}

func (m *InterfaceManager) allocateCachedInterface(netResourceStr string) (Resource, error) {
	netResource, err := NewNetResource(netResourceStr)
	if err != nil {
		m.releaseSlot()
		metrics.RecordResourcePoolAllocate(string(InterfaceResourceName), ResourcePoolAllocateError)
		recordPoolState(m)
		return netResource, err
	}
	if netResource.Interface == nil {
		if netResource.Ip != nil {
			m.idleIp.Push(netResource.Ip.String())
		}
		m.releaseSlot()
		metrics.RecordResourcePoolAllocate(string(InterfaceResourceName), ResourcePoolAllocateError)
		recordPoolState(m)
		return EmptyStringResource, fmt.Errorf("interface is nil for resource %s, this indicates a creation failure", netResourceStr)
	}

	validateStarted := time.Now()
	if err := m.validateInterface(netResource); err != nil {
		metrics.RecordResourceAllocateStage(string(InterfaceResourceName), "validate_cached", "error", time.Since(validateStarted).Seconds())
		logrus.Warnf("cached network interface %s is stale, rebuilding: %v", netResource.ToString(), err)
		staleResource := netResource
		rebuildStarted := time.Now()
		netResource, err = m.rebuildDevice(staleResource)
		if err != nil {
			metrics.RecordResourceAllocateStage(string(InterfaceResourceName), "rebuild_stale", "error", time.Since(rebuildStarted).Seconds())
			if staleResource.Ip != nil {
				m.idleIp.Push(staleResource.Ip.String())
			}
			m.releaseSlot()
			metrics.RecordResourcePoolAllocate(string(InterfaceResourceName), ResourcePoolAllocateError)
			recordPoolState(m)
			return EmptyStringResource, fmt.Errorf("rebuild stale interface %s failed: %w", netResourceStr, err)
		}
		metrics.RecordResourceAllocateStage(string(InterfaceResourceName), "rebuild_stale", "ok", time.Since(rebuildStarted).Seconds())
	} else {
		metrics.RecordResourceAllocateStage(string(InterfaceResourceName), "validate_cached", "ok", time.Since(validateStarted).Seconds())
	}

	m.markInterfaceUsing(netResource)
	metrics.RecordResourcePoolAllocate(string(InterfaceResourceName), ResourcePoolAllocateHit)
	if m.CacheNum() < m.CacheSizeLimit() {
		m.requestPoolRefill(ResourcePoolTriggerLowWatermark)
	}
	return netResource, nil
}

func (m *InterfaceManager) markInterfaceUsing(netResource *NetResource) {
	neighborStarted := time.Now()
	m.resetBridgeNeighbor(netResource.Ip, "allocate")
	metrics.RecordResourceAllocateStage(string(InterfaceResourceName), "neighbor_reset", "ok", time.Since(neighborStarted).Seconds())
	m.usingInterfaces.Set(netResource.ToString(), struct{}{})
	m.storeMark.Store(true)
}

func (m *InterfaceManager) Recycle(id string) error {
	m.initializeSlots()
	if _, owned := m.usingInterfaces.Pop(id); !owned {
		return nil
	}
	restoreOwnership := true
	defer func() {
		if restoreOwnership {
			m.usingInterfaces.Set(id, struct{}{})
		}
	}()
	netResource := &NetResource{}
	if err := netResource.FromString(id); err != nil {
		return fmt.Errorf("parse recycled network resource: %w", err)
	}
	if netResource.Interface == nil || netResource.Ip == nil || netResource.Ip.To4() == nil {
		return fmt.Errorf("recycled network resource is incomplete: %s", id)
	}
	if err := m.destroyInterface(*netResource.Interface); err != nil {
		return fmt.Errorf("destroy recycled interface %s: %w", netResource.Interface.Name, err)
	}

	restoreOwnership = false
	m.resetBridgeNeighbor(netResource.Ip, "recycle")
	m.idleIp.Push(netResource.Ip.String())
	m.releaseSlot()
	m.storeMark.Store(true)
	m.requestPoolRefill(ResourcePoolTriggerLowWatermark)
	logrus.Infof("retired interface after use: %s", netResource.ToString())
	return nil
}

func (m *InterfaceManager) Status() ([]string, []string) {
	var using []string
	var idle []string
	usingList := m.usingInterfaces.Keys()
	idleList := m.interfaces.List()
	for idx := range usingList {
		using = append(using, usingList[idx])
	}
	for idx := range idleList {
		idle = append(idle, idleList[idx])
	}
	return using, idle
}

func (m *InterfaceManager) buildOneResource() (*NetResource, error) {
	if !m.reserveSlot() {
		return nil, errord.ErrResourceExhausted
	}
	keepSlot := false
	defer func() {
		if !keepSlot {
			m.releaseSlot()
		}
	}()

	ip := m.idleIp.Pop()
	if ip == "" {
		return nil, errord.ErrResourceExhausted
	}
	if err := m.createInterfaceDevice(ip); err != nil {
		m.idleIp.Push(ip)
		return nil, err
	}

	hostVethName, _ := ipToVeth(ip)
	intf, err := m.lookupInterface(hostVethName)
	if err != nil {
		if cleanupErr := m.destroyInterface(net.Interface{Name: hostVethName}); cleanupErr != nil {
			// Keep the slot and IP quarantined while the host-side interface may
			// still exist. Reusing either could exceed capacity or duplicate an IP.
			keepSlot = true
			m.quarantinedSlots.Add(1)
			return nil, fmt.Errorf("lookup interface %s failed: %w; cleanup failed: %v", hostVethName, err, cleanupErr)
		}
		m.idleIp.Push(ip)
		return nil, fmt.Errorf("lookup interface %s failed: %w", hostVethName, err)
	}

	netResource := &NetResource{
		Interface: intf,
		Ip:        net.ParseIP(ip),
		Mask:      m.mask,
		Gateway:   m.BridgeIp,
		Type:      "bridge",
		NetNSPath: netnsPath(ip),
	}
	keepSlot = true
	logrus.Debugf("add network interface %v", netResource.ToString())
	return netResource, nil
}
