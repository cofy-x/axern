//go:build !linux

package ossloop

import "fmt"

func defaultRootfsMount(_, _, _, _ string) error {
	return fmt.Errorf("loop mount is only supported on linux")
}

func defaultRootfsUnmount(_, _ string) error {
	return fmt.Errorf("loop mount is only supported on linux")
}

func defaultMounted(_ string) (bool, error) {
	return false, nil
}
