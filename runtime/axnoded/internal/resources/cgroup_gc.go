package resources

import (
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/sirupsen/logrus"
)

func (c *CgroupManager) gc() {
	for {
		metrics.RecordGcQueueLength(string(CgroupResourceName), float64(c.gcQueue.Length()))
		cg := c.gcQueue.Pop()
		if cg == "" {
			time.Sleep(1 * time.Second)
			continue
		}
		if c.removeCgroupFromSystem(cg) != nil {
			logrus.Debugf("delete cgroup %v from gc queue failed, put it back to queue", cg)
			killCgroupProcesses(cg)
			c.gcQueue.Push(cg)
		} else {
			logrus.Debugf("delete cgroup %v from gc queue success", cg)
			c.generator.ReleaseId(cg)
		}
	}
}

func (c *CgroupManager) deleteCgroup(id string) {
	if !strings.Contains(id, c.rootName) {
		logrus.Debugf("cgroup %s is legal, does not belong to %s", id, c.rootName)
		return
	}

	c.cgroups.Remove(id)
	c.gcQueue.Push(id)
	c.storeMark.Store(true)
}

func (c *CgroupManager) removeCgroupFromSystem(name string) error {
	err := c.cgroupDriver.Remove(name)
	if err != nil && !os.IsNotExist(err) {
		logrus.Warningf("delete cgroup %s failed: %v", name, err)
		return err
	}
	return nil
}
