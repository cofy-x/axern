//go:build linux

package resources

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
)

func Test_runManager(t *testing.T) {
	rsm := &MockResourceManager{
		maxSize:      20,
		maxCacheSize: 18,
	}
	runManager(time.Second, rsm)
	time.Sleep(time.Second * 3)

	assert.Equal(t, 18, rsm.resourceCount)

	// Test for cgroup manager
	cgroupManager := &CgroupManager{
		size:                 10,
		rootName:             "sandbox",
		usingID:              cmap.New[struct{}](),
		idleID:               queue.New(""),
		cgroups:              cmap.New[struct{}](),
		generator:            nil,
		enableDestroyRecycle: false,
		storeMark:            atomic.Bool{},
		gcQueue:              queue.New(""),
	}

	cgroupManager.cgroups.Set("/sandbox/1", struct{}{})
	cgroupManager.cgroups.Set("/sandbox/2", struct{}{})
	cgroupManager.cgroups.Set("/sandbox/3", struct{}{})
	cgroupManager.cgroups.Set("/sandbox/4", struct{}{})
	cgroupManager.cgroups.Set("/sandbox/5", struct{}{})

	cgroupManager.usingID.Set("/sandbox/2", struct{}{})
	cgroupManager.usingID.Set("/sandbox/3", struct{}{})
	cgroupManager.usingID.Set("/sandbox/5", struct{}{})

	// Test recycle not exist, should ignore
	cgroupManager.Recycle("1")
	assert.Equal(t, 5, cgroupManager.cgroups.Count())
	assert.Equal(t, 0, cgroupManager.idleID.Length())

	// Test recycle used with reuse mode, should reuse
	cgroupManager.enableDestroyRecycle = false
	cgroupManager.Recycle("/sandbox/2")
	assert.Equal(t, 5, cgroupManager.cgroups.Count())
	assert.False(t, cgroupManager.usingID.Has("/sandbox/2"))
	assert.Equal(t, 1, cgroupManager.idleID.Length())

	// Test recycle used, should delete
	cgroupManager.enableDestroyRecycle = true
	cgroupManager.Recycle("/sandbox/3")
	assert.Equal(t, 4, cgroupManager.cgroups.Count())
	assert.False(t, cgroupManager.usingID.Has("/sandbox/3"))
	assert.Equal(t, 1, cgroupManager.idleID.Length())
	assert.False(t, cgroupManager.idleID.Has("/sandbox/3"))

	// Test recycle twice concurrently
	cgroupManager.enableDestroyRecycle = false
	cgroupManager.usingID.Set("/sandbox/4", struct{}{})
	cgroupManager.Recycle("/sandbox/4")
	assert.Equal(t, 2, cgroupManager.idleID.Length())
	assert.True(t, cgroupManager.idleID.Has("/sandbox/4"))
	assert.Equal(t, 4, cgroupManager.cgroups.Count())
	cgroupManager.Recycle("/sandbox/4")
	assert.Equal(t, 2, cgroupManager.idleID.Length())
	assert.Equal(t, 4, cgroupManager.cgroups.Count())
	assert.Equal(t, 4, cgroupManager.cgroups.Count())
	assert.False(t, cgroupManager.usingID.Has("/sandbox/4"))

	manager, err := NewResourceManager(storetest.NewMockStore(), config.Config{
		PluginConfig: config.PluginConfig{
			ResourceConfig: config.ResourceConfig{
				MaxInstanceNum:     10,
				CgroupRootName:     "sandbox",
				CgroupCacheSize:    8,
				InterfaceCacheSize: 0,
				ResourceAdvanceConfig: config.ResourceAdvanceConfig{
					RecyclePolicy: config.RecyclePolicyDestroy,
				},
			},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(manager))
}

func Test_runManagerCal(t *testing.T) {
	rsm := &MockResourceManager{
		maxSize:      10,
		maxCacheSize: 5,
	}
	runManager(time.Millisecond, rsm)
	time.Sleep(time.Millisecond * 500)
	reconcilePool(rsm)
	assert.Equal(t, 5, rsm.resourceCount)
}

func Test_Resize(t *testing.T) {
	rsm := &MockResourceManager{
		maxSize:      10,
		maxCacheSize: 5,
	}
	reconcilePool(rsm)
	_, err := rsm.Allocate(AllocateOption{ContainerID: "error"})
	assert.Error(t, err)
	_, err = rsm.Allocate(AllocateOption{ContainerID: "1"})
	assert.NoError(t, err)
	_, err = rsm.Allocate(AllocateOption{ContainerID: "1"})
	assert.NoError(t, err)
	_, err = rsm.Allocate(AllocateOption{ContainerID: "1"})
	assert.NoError(t, err)
	reconcilePool(rsm)
	assert.Equal(t, 5, rsm.resourceCount)
	assert.Equal(t, 5, rsm.CacheNum())
	assert.Equal(t, 3, rsm.UsingNum())
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	reconcilePool(rsm)
	assert.Equal(t, 5, rsm.CacheNum())
	assert.Equal(t, 6, rsm.UsingNum())
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	reconcilePool(rsm)
	assert.Equal(t, 2, rsm.CacheNum())
	assert.Equal(t, 9, rsm.UsingNum())
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	reconcilePool(rsm)
	assert.Equal(t, 1, rsm.CacheNum())
	assert.Equal(t, 10, rsm.UsingNum())
	_, err = rsm.Allocate(AllocateOption{ContainerID: "1"})
	assert.Nil(t, err)
	assert.Error(t, rsm.Recycle("error"))
	rsm.Recycle("1")
	rsm.Recycle("1")
	rsm.Recycle("1")
	reconcilePool(rsm)
	assert.Equal(t, 3, rsm.CacheNum())
	assert.Equal(t, 8, rsm.UsingNum())
	rsm.Recycle("1")
	rsm.Recycle("1")
	rsm.Recycle("1")
	reconcilePool(rsm)
	assert.Equal(t, 5, rsm.UsingNum())
	assert.Equal(t, 6, rsm.CacheNum())
	rsm.Recycle("1")
	rsm.Recycle("1")
	rsm.Recycle("1")
	rsm.Recycle("1")
	rsm.Recycle("1")
	reconcilePool(rsm)
	assert.Equal(t, 11, rsm.CacheNum())
	assert.Equal(t, 0, rsm.UsingNum())
}
