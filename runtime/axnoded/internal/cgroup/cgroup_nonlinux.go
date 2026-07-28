//go:build !linux

package cgroup

import (
	"fmt"
	"runtime"
)

func newDefaultCgroupDriver() (CgroupDriver, error) {
	return nil, fmt.Errorf("cgroup driver is unsupported on %s", runtime.GOOS)
}
