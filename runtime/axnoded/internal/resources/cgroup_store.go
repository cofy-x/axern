package resources

import (
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/sirupsen/logrus"
)

func NewCgroupManager(
	db stateStore,
	cfg config.ResourceConfig,
) (*CgroupManager, error) {

	rootName := cfg.CgroupRootName
	if rootName == "" {
		rootName = config.DefaultCgroupRoot
	}
	cgroupDriver, err := os2.DefaultCgroupDriver()
	if err != nil {
		return nil, err
	}
	// load using id from db
	var usingID apipb.Slice
	err = db.LoadSnapshot(config.CgroupBucket, &usingID)
	if err != nil && !errord.IsNotFound(err) {
		return nil, err
	}
	if err == nil {
		logrus.Infof("load cgroup using id num: %v", len(usingID.Items))
	}

	// load all cgroup under rootName
	cgs, err := loadAllCgroups(cgroupDriver, rootName)
	if err != nil {
		return nil, err
	}
	if cgs.Count() > 0 {
		logrus.Infof("load existsing cgroup num: %v", cgs.Count())
	}
	idleIDs := queue.New("")
	usingIDs := cmap.New[struct{}]()
	for _, id := range usingID.Items {
		usingIDs.Set(id, struct{}{})
	}

	c := &CgroupManager{
		size:                 cfg.MaxInstanceNum,
		cacheSize:            cfg.CgroupCacheSize,
		rootName:             rootName,
		usingID:              usingIDs,
		idleID:               idleIDs,
		gcQueue:              queue.New(""),
		generator:            truncindex.NewFixLenGenerator(12, cgs.Keys(), truncindex.PrefixModifier(filepath.Join("/", rootName)+"/")),
		db:                   db,
		cgroups:              cgs,
		storeMark:            atomic.Bool{},
		storeStop:            make(chan struct{}),
		storeDone:            make(chan struct{}),
		enableDestroyRecycle: cfg.RecyclePolicy == config.RecyclePolicyDestroy,
		cgroupDriver:         cgroupDriver,
	}

	for id := range cgs.Items() {
		if usingIDs.Has(id) {
			continue
		}
		c.deleteCgroup(id)
	}
	c.keepStoring()
	go c.gc()

	return c, nil
}

func (c *CgroupManager) keepStoring() {
	go func() {
		defer close(c.storeDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.storeMark.Load() {
					c.storeMark.Store(false)
					c.store()
				}
			case <-c.storeStop:
				return
			}
		}
	}()
}

func (c *CgroupManager) stopStoreLoop() {
	if c.storeStop == nil || c.storeDone == nil {
		return
	}
	c.storeOnce.Do(func() { close(c.storeStop) })
	<-c.storeDone
}

func (c *CgroupManager) store() {
	start := time.Now()
	defer func() {
		logrus.Debugf("store cgroup %v using id cost: %v ms", c.usingID.Count(), time.Since(start).Milliseconds())
	}()
	dataToStore := &apipb.Slice{Items: c.usingID.Keys()}
	if err := c.db.SaveSnapshot(config.CgroupBucket, dataToStore); err != nil {
		logrus.Warnf("store cgroup using id failed: %v", err)
	}
}

// load all cgroup under rootName from blkio subsystem
func loadAllCgroups(driver os2.CgroupDriver, rootName string) (cmap.ConcurrentMap[string, struct{}], error) {
	groupDirs, err := driver.ExistingGroups(rootName)
	if err != nil {
		return cmap.New[struct{}](), err
	}
	cgroups := cmap.New[struct{}]()
	for _, dir := range groupDirs {
		cgroups.Set(dir, struct{}{})
	}
	return cgroups, nil
}
