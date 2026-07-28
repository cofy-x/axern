//go:build !linux

package oci

import "fmt"

func mountReadonlyOverlay(_, _ string) error {
	return fmt.Errorf("overlay mount is only supported on linux")
}

func mountReadonlyBind(_, _ string) error {
	return fmt.Errorf("bind mount is only supported on linux")
}

func unmountOverlay(_ string) error {
	return fmt.Errorf("overlay unmount is only supported on linux")
}
