//go:build linux

package oci

import "golang.org/x/sys/unix"

func mountReadonlyOverlay(target, mountOpts string) error {
	// Readonly is guaranteed by lowerdir-only overlay; passing MS_RDONLY can
	// trigger EINVAL on some kernels for overlay mounts.
	return unix.Mount("overlay", target, "overlay", 0, mountOpts)
}

func mountReadonlyBind(source, target string) error {
	if err := unix.Mount(source, target, "", uintptr(unix.MS_BIND), ""); err != nil {
		return err
	}
	return unix.Mount("", target, "", uintptr(unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY), "")
}

func unmountOverlay(target string) error {
	return unix.Unmount(target, 0)
}
