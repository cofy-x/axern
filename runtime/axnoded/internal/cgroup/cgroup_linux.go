//go:build linux

package cgroup

import (
	stdos "os"
	"path"
)

func newDefaultCgroupDriver() (CgroupDriver, error) {
	if isCgroupV2Unified() {
		delegationGroup, err := currentUnifiedGroup()
		if err != nil {
			return nil, err
		}
		if path.Base(delegationGroup) == cgroupInternalGroup {
			delegationGroup = path.Dir(delegationGroup)
		}
		return &cgroupV2Driver{mountpoint: "/sys/fs/cgroup", delegationGroup: normalizeGroup(delegationGroup)}, nil
	}
	return &cgroupV1Driver{}, nil
}

func isCgroupV2Unified() bool {
	_, err := stdos.Stat("/sys/fs/cgroup/cgroup.controllers")
	return err == nil
}
