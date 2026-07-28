/**
 * Alipay.com Inc.
 * Copyright (c) 2004-2025 All Rights Reserved.
 */

package resources

import (
	"errors"
	"net"
	"runtime"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
)

func initInterfaceCache() InterfaceManager {
	return InterfaceManager{
		allInterfaces: []net.Interface{
			{Name: "eth0"},
			{Name: "eth2"},
		},
	}
}

func initInterfaceCacheForCleanup() *InterfaceManager {
	im := &InterfaceManager{
		interfaces:      queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
	}

	im.interfaces.Push(`{"interface":{"Index":21298,"MTU":1500,"Name":"pv.ac1100ad","HardwareAddr":"+gJCgdfr","Flags":51},"ip":"172.17.0.173","mask":"//8AAA==","gateway":"172.17.0.1","type":"bridge"}`)
	im.interfaces.Push("")
	im.interfaces.Push("invalid interface")
	im.interfaces.Push(`{"ip":"172.17.0.174","mask":"//8AAA==","gateway":"172.17.0.1","type":"bridge"}`)

	return im
}

func TestInterfaceCleanup(t *testing.T) {
	m := initInterfaceCacheForCleanup()
	m.destroyDeviceFunc = func(net.Interface) error {
		return errors.New("fake destroyDevice error")
	}

	m.cleanup()

	m.destroyDeviceFunc = func(net.Interface) error {
		return nil
	}

	m.cleanup()
}

func TestCalcluteCacheSize(t *testing.T) {
	rawCacheSize := 10000
	cacheSize := calcluteCacheSizeWithCPUProbe(rawCacheSize, func() (int, error) {
		return 2, nil
	})
	assert.True(t, cacheSize < rawCacheSize)
}

func TestCalcluteCacheSizeOnCPUProbeError(t *testing.T) {
	rawCacheSize := 10000
	cacheSize := calcluteCacheSizeWithCPUProbe(rawCacheSize, func() (int, error) {
		return 1, errors.New("fake error for getLocalCpuNum")
	})
	assert.Equal(t, rawCacheSize, cacheSize)
}

func TestDestroyDevice(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("destroyDevice requires Linux netlink")
	}
	m := initInterfaceCache()

	err := m.destroyDevice(m.allInterfaces[0])
	assert.Nil(t, err)
}
