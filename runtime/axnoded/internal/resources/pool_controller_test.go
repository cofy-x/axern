//go:build linux

package resources

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_runManager(t *testing.T) {
	rsm := &MockResourceManager{
		maxSize:      20,
		maxCacheSize: 18,
	}
	runManager(time.Second, rsm)
	t.Cleanup(func() { require.NoError(t, rsm.ShutDown()) })
	time.Sleep(time.Second * 3)

	assert.Equal(t, 18, rsm.resourceCount)
}

func Test_runManagerCal(t *testing.T) {
	rsm := &MockResourceManager{
		maxSize:      10,
		maxCacheSize: 5,
	}
	runManager(time.Millisecond, rsm)
	t.Cleanup(func() { require.NoError(t, rsm.ShutDown()) })
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
	assert.Equal(t, 4, rsm.CacheNum())
	assert.Equal(t, 6, rsm.UsingNum())
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	reconcilePool(rsm)
	assert.Equal(t, 1, rsm.CacheNum())
	assert.Equal(t, 9, rsm.UsingNum())
	rsm.Allocate(AllocateOption{ContainerID: "1"})
	reconcilePool(rsm)
	assert.Equal(t, 0, rsm.CacheNum())
	assert.Equal(t, 10, rsm.UsingNum())
	_, err = rsm.Allocate(AllocateOption{ContainerID: "1"})
	assert.Error(t, err)
	assert.Error(t, rsm.Recycle("error"))
	rsm.Recycle("1")
	rsm.Recycle("1")
	rsm.Recycle("1")
	reconcilePool(rsm)
	assert.Equal(t, 3, rsm.CacheNum())
	assert.Equal(t, 7, rsm.UsingNum())
	rsm.Recycle("1")
	rsm.Recycle("1")
	rsm.Recycle("1")
	reconcilePool(rsm)
	assert.Equal(t, 4, rsm.UsingNum())
	assert.Equal(t, 6, rsm.CacheNum())
	rsm.Recycle("1")
	rsm.Recycle("1")
	rsm.Recycle("1")
	rsm.Recycle("1")
	reconcilePool(rsm)
	assert.Equal(t, 10, rsm.CacheNum())
	assert.Equal(t, 0, rsm.UsingNum())
}
