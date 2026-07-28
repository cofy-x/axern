// Package cgroup manages a memory cgroup for imagefsd daemon processes.
//
// On Linux, it creates a /imagefsd cgroup (v1 or v2) and places daemon
// processes into it. On other platforms, all operations are no-ops.
// A nil *Controller is safe to use — all methods become no-ops.
package cgroup

import (
	"os/exec"
	"sync/atomic"
)

// Controller manages a cgroup for imagefsd daemons.
// A nil *Controller is safe to use; all methods become no-ops.
type Controller struct {
	cgroupVersion int    // 1 or 2
	cgroupDir     string // path to the cgroup directory
	cgroupFD      int    // file descriptor for cgroup dir (v2 only, -1 if unused)
	useCgroupFD   atomic.Bool
}

// Enabled reports whether the controller is active.
func (c *Controller) Enabled() bool {
	return c != nil
}

// Apply configures cmd to start directly in the cgroup (v2) or is a no-op (v1/nil).
// It returns true when direct cgroup-fd placement was requested.
// Must be called before cmd.Start().
func (c *Controller) Apply(cmd *exec.Cmd) bool {
	if c == nil {
		return false
	}
	return c.applyPlatform(cmd)
}

// AddPID writes a process ID to cgroup.procs.
// On v2 this becomes the fallback mechanism when direct placement is disabled.
// On v1 this is the primary mechanism, called after the daemon is confirmed running.
func (c *Controller) AddPID(pid int) error {
	if c == nil {
		return nil
	}
	return c.addPIDPlatform(pid)
}

// Close releases resources held by the controller (e.g. cgroup dir fd).
func (c *Controller) Close() error {
	if c == nil {
		return nil
	}
	return c.closePlatform()
}

// DisableDirectPlacement permanently disables clone3/cgroup-fd placement and
// switches v2 controllers to cgroup.procs writes for subsequent processes.
func (c *Controller) DisableDirectPlacement() {
	if c == nil {
		return
	}
	c.useCgroupFD.Store(false)
}
