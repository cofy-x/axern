package oci

import (
	"fmt"
	"path"
	"slices"
	"strings"

	spec "github.com/opencontainers/runtime-spec/specs-go"
)

const guestCgroupMountPath = "/sys/fs/cgroup"

// applyTaskLimits runs after request and template overrides so only the trusted
// execution profile owns the host limit and guest cgroup observation boundary.
func (p ExecutionProfile) applyTaskLimits(ociSpec *spec.Spec) error {
	if p.GuestTaskLimit == 0 {
		return nil
	}
	if ociSpec.Process != nil && ociSpec.Process.Capabilities != nil {
		caps := ociSpec.Process.Capabilities
		for _, set := range [][]string{caps.Bounding, caps.Effective, caps.Permitted, caps.Inheritable, caps.Ambient} {
			if slices.Contains(set, "CAP_SYS_ADMIN") {
				return fmt.Errorf("task limits require an OCI process without CAP_SYS_ADMIN")
			}
		}
	}
	for _, mount := range ociSpec.Mounts {
		if strings.HasPrefix(path.Clean(mount.Destination), guestCgroupMountPath+"/") {
			return fmt.Errorf("mount destination %s conflicts with the task limit cgroup mount", mount.Destination)
		}
	}
	if ociSpec.Linux.Resources == nil {
		ociSpec.Linux.Resources = &spec.LinuxResources{}
	}
	hostTaskLimit := p.HostTaskLimit
	ociSpec.Linux.Resources.Pids = &spec.LinuxPids{Limit: &hostTaskLimit}
	delete(ociSpec.Linux.Resources.Unified, "pids.max")
	ociSpec.Linux.Namespaces = slices.DeleteFunc(ociSpec.Linux.Namespaces, func(ns spec.LinuxNamespace) bool {
		return ns.Type == spec.CgroupNamespace
	})
	ociSpec.Linux.Namespaces = append(ociSpec.Linux.Namespaces, spec.LinuxNamespace{Type: spec.CgroupNamespace})
	ociSpec.Mounts = slices.DeleteFunc(ociSpec.Mounts, func(mount spec.Mount) bool {
		return path.Clean(mount.Destination) == guestCgroupMountPath
	})
	// Appending last also prevents an ancestor mount from hiding this mount.
	ociSpec.Mounts = append(ociSpec.Mounts, spec.Mount{
		Destination: guestCgroupMountPath,
		Type:        "cgroup2",
		Source:      "cgroup2",
		Options:     []string{"ro", "nosuid", "noexec", "nodev"},
	})
	return nil
}
