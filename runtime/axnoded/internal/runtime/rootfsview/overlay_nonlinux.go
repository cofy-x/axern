//go:build !linux

package rootfsview

import "fmt"

func InspectBacking(rootDir string) (RootfsBackingFacts, error) {
	return RootfsBackingFacts{
		EffectiveRoot: rootDir, Mountpoint: rootDir, FSType: "unknown", LowerDirs: []string{rootDir},
		EffectiveLowerChain: []RootfsBackingLayerFacts{{Path: rootDir, Mountpoint: rootDir, FSType: "unknown"}},
	}, nil
}

func verifyMountedOverlay(path string) error {
	return fmt.Errorf("verify OverlayFS mount %s: unsupported platform", path)
}

func resolveOverlayLowerDirs(rootDir string) ([]string, error) {
	return []string{rootDir}, nil
}

func mountOverlayView(rootfs overlayView) error {
	return fmt.Errorf("writable rootfs view is only supported on linux")
}

func unmountOverlayView(rootfs overlayView) error {
	return nil
}
