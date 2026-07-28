//go:build !linux

package rootfsview

import "fmt"

func resolveOverlayLowerDirs(rootDir string) ([]string, error) {
	return []string{rootDir}, nil
}

func mountOverlayView(rootfs overlayView) error {
	return fmt.Errorf("writable rootfs view is only supported on linux")
}

func unmountOverlayView(rootfs overlayView) error {
	return nil
}
