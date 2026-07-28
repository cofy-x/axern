package resources

import (
	"sync"
	"sync/atomic"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	cmap "github.com/orcaman/concurrent-map/v2"
)

type CgroupManager struct {
	size      int
	cacheSize int
	rootName  string

	poolController *poolController

	usingID cmap.ConcurrentMap[string, struct{}]
	idleID  *queue.Queue[string]

	// It maintains all cgroups under /huse before axnoded starts and all cgroups created by axnoded
	// All used/reused cgroups must be in this list.
	cgroups   cmap.ConcurrentMap[string, struct{}]
	generator truncindex.UniqueIdGenerator
	// if enableDestroyRecycle is true, the cgroup will be destroyed when be recycled.
	enableDestroyRecycle bool

	sync.Mutex
	db stateStore

	// storeMark is used to mark whether the cgroup id need to be stored.
	// If it's true, manager should not exit.
	storeMark atomic.Bool
	storeStop chan struct{}
	storeDone chan struct{}
	storeOnce sync.Once

	gcQueue *queue.Queue[string]

	// cgroupDriver abstracts host cgroup operations for platform implementations and tests.
	cgroupDriver os2.CgroupDriver
}

const RetryGenIdTimes = 100

func (c *CgroupManager) MaxSizeLimit() int {
	if c.size == 0 {
		return config.DefaultMaxContainerNum
	}
	return c.size
}

func (c *CgroupManager) CacheSizeLimit() int {
	if c.cacheSize == 0 {
		return config.DefaultMaxCacheLimitNum
	}
	return c.cacheSize
}

func (c *CgroupManager) UsingNum() int {
	c.Lock()
	defer c.Unlock()
	return c.usingID.Count()
}

func (c *CgroupManager) ShutDown() error {
	c.stopStoreLoop()
	if c.storeMark.Load() {
		c.store()
	}
	return nil
}

func (c *CgroupManager) Status() ([]string, []string) {
	c.Lock()
	defer c.Unlock()
	return c.usingID.Keys(), c.idleID.List()
}

func (c *CgroupManager) MaxSize() int {
	if c.size == 0 {
		return config.DefaultMaxContainerNum
	}
	return c.size
}

func (c *CgroupManager) ResourceName() ResourceName {
	return CgroupResourceName
}

func (c *CgroupManager) CacheNum() int {
	return c.idleID.Length()
}

func (c *CgroupManager) setPoolController(controller *poolController) {
	c.poolController = controller
	recordPoolState(c)
}

func (c *CgroupManager) requestPoolRefill(trigger string) {
	if c.poolController != nil {
		c.poolController.request(trigger)
	}
}

var _ Manager = &CgroupManager{}
