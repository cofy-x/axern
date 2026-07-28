package resources

import (
	"errors"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

func (c *CgroupManager) Recycle(id string) error {
	if _, owned := c.usingID.Pop(id); !owned {
		return nil
	}
	defer c.storeMark.Store(true)
	if c.enableDestroyRecycle {
		c.deleteCgroup(id)
		recordPoolState(c)
		return nil
	}
	err := c.recycleWithReuse(id)
	recordPoolState(c)
	return err
}

func (c *CgroupManager) recycleWithReuse(id string) error {
	// Never recycle cgroups which aren't in c.cgroups
	if !c.cgroups.Has(id) {
		return nil
	}
	c.idleID.Push(id)
	return nil
}

func (c *CgroupManager) Allocate(opt AllocateOption) (Resource, error) {
	id := c.idleID.Pop()
	if id != "" {
		c.usingID.Set(id, struct{}{})
		c.storeMark.Store(true)
		metrics.RecordResourcePoolAllocate(string(CgroupResourceName), ResourcePoolAllocateHit)
		if c.CacheNum() < c.CacheSizeLimit() {
			c.requestPoolRefill(ResourcePoolTriggerLowWatermark)
		}
		return NewStringResource(id), nil
	}

	c.requestPoolRefill(ResourcePoolTriggerAllocationMiss)
	id, err := c.createOneSync()
	if err != nil {
		result := ResourcePoolAllocateError
		if errors.Is(err, errord.ErrResourceExhausted) {
			result = ResourcePoolAllocateExhausted
		}
		metrics.RecordResourcePoolAllocate(string(CgroupResourceName), result)
		recordPoolState(c)
		return EmptyStringResource, err
	}

	c.usingID.Set(id, struct{}{})
	c.storeMark.Store(true)
	metrics.RecordResourcePoolAllocate(string(CgroupResourceName), ResourcePoolAllocateMissSyncCreate)
	c.requestPoolRefill(ResourcePoolTriggerAllocationMiss)
	return NewStringResource(id), nil
}

func (c *CgroupManager) Add(num int) int {
	logrus.Debugf("start to add %v cgroups", num)
	c.Lock()
	added := c.addLocked(num)
	c.Unlock()
	recordPoolState(c)
	logrus.Debugf("add %v cgroups done", num)
	return added
}

func (c *CgroupManager) addLocked(num int) int {
	added := 0
	for i := 0; i < num; i++ {
		newID, err := c.createOneLocked()
		if err != nil {
			logrus.Errorf("create cgroup failed: %v", err)
			continue
		}
		c.idleID.Push(newID)
		added++
	}
	return added
}

func (c *CgroupManager) Del(num int) {
	if c.idleID.Num() == 0 {
		return
	}
	logrus.Debugf("start to delete %v cgroups", num)
	for i := 0; i < num; i++ {
		id := c.idleID.Pop()
		if id == "" {
			break
		}
		c.deleteCgroup(id)
	}
	recordPoolState(c)
	logrus.Debugf("delete %v cgroups done", num)
}

func (c *CgroupManager) createOneSync() (string, error) {
	c.Lock()
	defer c.Unlock()
	return c.createOneLocked()
}

func (c *CgroupManager) createOneLocked() (string, error) {
	if c.cgroups.Count() >= c.MaxSizeLimit() {
		return "", errord.ErrResourceExhausted
	}
	newID, err := c.generator.GetID()
	if err != nil {
		return "", err
	}
	_, err = c.cgroupDriver.Create(newID, &specs.LinuxResources{})
	if err != nil {
		c.generator.ReleaseId(newID)
		return "", err
	}

	logrus.Debugf("create cgroup %v success", newID)
	c.cgroups.Set(newID, struct{}{})
	c.storeMark.Store(true)
	return newID, nil
}
