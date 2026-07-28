//go:build !linux

package cgroup

import (
	"os/exec"

	"github.com/sirupsen/logrus"
)

// NewController is a no-op on non-Linux platforms.
func NewController(memoryLimitBytes int64) *Controller {
	logrus.Debug("cgroup: not supported on this platform, cgroup management disabled")
	return nil
}

func (c *Controller) applyPlatform(cmd *exec.Cmd) bool { return false }

func (c *Controller) addPIDPlatform(pid int) error { return nil }

func (c *Controller) closePlatform() error { return nil }
