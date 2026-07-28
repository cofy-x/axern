//go:build linux

package cgroup

import stdos "os"

func newDefaultCgroupDriver() (CgroupDriver, error) {
	if isCgroupV2Unified() {
		return &cgroupV2Driver{mountpoint: "/sys/fs/cgroup"}, nil
	}
	return &cgroupV1Driver{}, nil
}

func isCgroupV2Unified() bool {
	_, err := stdos.Stat("/sys/fs/cgroup/cgroup.controllers")
	return err == nil
}
