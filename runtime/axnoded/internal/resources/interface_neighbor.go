package resources

import (
	"errors"
	"net"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	neighborResetCleared = "cleared"
	neighborResetAbsent  = "absent"
	neighborResetError   = "error"
)

func (m *InterfaceManager) resetBridgeNeighbor(ip net.IP, trigger string) {
	if m == nil || ip == nil || (m.deleteNeighborFunc == nil && m.bridgeLink == nil) {
		return
	}
	err := m.deleteBridgeNeighbor(ip)
	result := neighborResetCleared
	if isMissingNeighbor(err) {
		result = neighborResetAbsent
		err = nil
	} else if err != nil {
		result = neighborResetError
	}
	metrics.RecordNetworkNeighborReset(trigger, result)
	if err != nil {
		logrus.Warnf("reset bridge neighbor for %s during %s failed: %v", ip, trigger, err)
	}
}

func (m *InterfaceManager) deleteBridgeNeighbor(ip net.IP) error {
	if m.deleteNeighborFunc != nil {
		return m.deleteNeighborFunc(ip)
	}
	attrs := m.bridgeLink.Attrs()
	if attrs == nil || attrs.Index <= 0 {
		return errors.New("bridge link has no index")
	}
	return netlink.NeighDel(&netlink.Neigh{LinkIndex: attrs.Index, IP: ip})
}

func isMissingNeighbor(err error) bool {
	return errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESRCH)
}
