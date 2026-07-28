package resources

import (
	"errors"
	"net"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
)

func TestResetBridgeNeighborRecordsResults(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantResult string
	}{
		{name: "cleared", wantResult: neighborResetCleared},
		{name: "absent", err: unix.ENOENT, wantResult: neighborResetAbsent},
		{name: "error", err: errors.New("netlink unavailable"), wantResult: neighborResetError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics.ResetForTest()
			manager := &InterfaceManager{deleteNeighborFunc: func(net.IP) error { return tt.err }}

			manager.resetBridgeNeighbor(net.ParseIP("172.17.0.2"), "allocate")

			assert.Equal(t, float64(1), metrics.CounterValueForTest(
				metrics.MetricNetworkNeighborResetTotal,
				map[string]string{"axern.trigger": "allocate", "axern.result": tt.wantResult},
			))
		})
	}
}

func TestInterfaceManagerRecycleRetiresUsedInterfaceAndRefillsPool(t *testing.T) {
	var resetIP string
	var destroyedName string
	destroyCalls := 0
	manager := &InterfaceManager{
		interfaces:      queue.New(""),
		idleIp:          queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
		destroyDeviceFunc: func(dev net.Interface) error {
			destroyCalls++
			destroyedName = dev.Name
			return nil
		},
		deleteNeighborFunc: func(ip net.IP) error {
			resetIP = ip.String()
			return nil
		},
	}
	resource := (&NetResource{
		Interface: &net.Interface{Name: "hv.ac110003"},
		Ip:        net.ParseIP("172.17.0.3"),
	}).ToString()
	manager.usingInterfaces.Set(resource, struct{}{})

	assert.NoError(t, manager.Recycle(resource))
	assert.Equal(t, "172.17.0.3", resetIP)
	assert.Equal(t, "hv.ac110003", destroyedName)
	assert.False(t, manager.usingInterfaces.Has(resource))
	assert.Equal(t, "172.17.0.3", manager.idleIp.Pop())
	assert.Equal(t, 0, manager.interfaces.Length())
	assert.NoError(t, manager.Recycle(resource))
	assert.Equal(t, 1, destroyCalls)
	assert.Equal(t, 0, manager.idleIp.Length())
}

func TestInterfaceManagerRecycleKeepsOwnershipForInvalidResource(t *testing.T) {
	manager := &InterfaceManager{
		interfaces:      queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
	}
	const resource = "invalid"
	manager.usingInterfaces.Set(resource, struct{}{})

	assert.Error(t, manager.Recycle(resource))
	assert.True(t, manager.usingInterfaces.Has(resource))
	assert.Equal(t, 0, manager.interfaces.Length())
}

func TestInterfaceManagerRecycleTreatsAlreadyRemovedDeviceAsReleased(t *testing.T) {
	manager := &InterfaceManager{
		interfaces:      queue.New(""),
		idleIp:          queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
		destroyDeviceFunc: func(net.Interface) error {
			return unix.ENODEV
		},
	}
	resource := (&NetResource{
		Interface: &net.Interface{Name: "hv.ac110004"},
		Ip:        net.ParseIP("172.17.0.4"),
	}).ToString()
	manager.usingInterfaces.Set(resource, struct{}{})
	manager.activeSlots.Store(1)

	assert.NoError(t, manager.Recycle(resource))
	assert.False(t, manager.usingInterfaces.Has(resource))
	assert.Equal(t, "172.17.0.4", manager.idleIp.Pop())
	assert.Equal(t, int64(0), manager.activeSlots.Load())
}
