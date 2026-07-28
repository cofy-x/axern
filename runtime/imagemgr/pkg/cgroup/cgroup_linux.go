//go:build linux

package cgroup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/sirupsen/logrus"
)

const (
	cgroupV2Root = "/sys/fs/cgroup"
	cgroupV1Root = "/sys/fs/cgroup/memory"
	cgroupName   = "imagefsd"
)

// NewController creates a cgroup for imagefsd daemons.
// memoryLimitBytes sets a memory limit (0 means no limit).
// Returns nil on any failure (logs a warning); callers should treat nil as disabled.
func NewController(memoryLimitBytes int64) *Controller {
	version := detectCgroupVersion()
	if version == 0 {
		logrus.Warn("cgroup: unable to detect cgroup version, cgroup management disabled")
		return nil
	}

	c := &Controller{
		cgroupVersion: version,
		cgroupFD:      -1,
	}

	var err error
	switch version {
	case 2:
		c.cgroupDir = filepath.Join(cgroupV2Root, cgroupName)
		err = c.initV2(memoryLimitBytes)
	case 1:
		c.cgroupDir = filepath.Join(cgroupV1Root, cgroupName)
		err = c.initV1(memoryLimitBytes)
	}

	if err != nil {
		logrus.Warnf("cgroup: failed to initialize v%d cgroup: %v", version, err)
		c.closePlatform()
		return nil
	}

	logrus.Infof("cgroup: v%d controller initialized at %s (memory_limit=%d)", version, c.cgroupDir, memoryLimitBytes)
	return c
}

// detectCgroupVersion returns 2 for cgroup v2, 1 for v1, 0 if unknown.
func detectCgroupVersion() int {
	// If /sys/fs/cgroup/cgroup.controllers exists, it's v2 (unified hierarchy).
	if _, err := os.Stat(filepath.Join(cgroupV2Root, "cgroup.controllers")); err == nil {
		return 2
	}
	// If /sys/fs/cgroup/memory exists, it's v1.
	if info, err := os.Stat(cgroupV1Root); err == nil && info.IsDir() {
		return 1
	}
	return 0
}

func (c *Controller) initV2(memoryLimitBytes int64) error {
	if err := os.MkdirAll(c.cgroupDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", c.cgroupDir, err)
	}

	if memoryLimitBytes > 0 {
		memMaxPath := filepath.Join(c.cgroupDir, "memory.max")
		if err := os.WriteFile(memMaxPath, []byte(strconv.FormatInt(memoryLimitBytes, 10)), 0644); err != nil {
			return fmt.Errorf("set memory.max: %w", err)
		}
	}

	// Open the cgroup dir fd for SysProcAttr.CgroupFD.
	fd, err := syscall.Open(c.cgroupDir, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return fmt.Errorf("open cgroup dir fd: %w", err)
	}
	c.cgroupFD = fd
	c.useCgroupFD.Store(true)
	return nil
}

func (c *Controller) initV1(memoryLimitBytes int64) error {
	if err := os.MkdirAll(c.cgroupDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", c.cgroupDir, err)
	}

	if memoryLimitBytes > 0 {
		limitPath := filepath.Join(c.cgroupDir, "memory.limit_in_bytes")
		if err := os.WriteFile(limitPath, []byte(strconv.FormatInt(memoryLimitBytes, 10)), 0644); err != nil {
			return fmt.Errorf("set memory.limit_in_bytes: %w", err)
		}
	}
	return nil
}

// applyPlatform sets CgroupFD on the command for v2 (clone3 CLONE_INTO_CGROUP).
func (c *Controller) applyPlatform(cmd *exec.Cmd) bool {
	if c.cgroupVersion != 2 || c.cgroupFD < 0 || !c.useCgroupFD.Load() {
		return false
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = c.cgroupFD
	return true
}

// addPIDPlatform writes PID to cgroup.procs (v1 primary mechanism).
func (c *Controller) addPIDPlatform(pid int) error {
	if c.cgroupVersion == 2 && c.useCgroupFD.Load() {
		// v2 primary path: processes are placed via Apply/CgroupFD.
		return nil
	}
	procsPath := filepath.Join(c.cgroupDir, "cgroup.procs")
	return os.WriteFile(procsPath, []byte(strconv.Itoa(pid)), 0644)
}

// closePlatform releases the cgroup directory fd.
func (c *Controller) closePlatform() error {
	if c.cgroupFD >= 0 {
		err := syscall.Close(c.cgroupFD)
		c.cgroupFD = -1
		return err
	}
	return nil
}
