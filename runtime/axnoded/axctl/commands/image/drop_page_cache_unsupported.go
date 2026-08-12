//go:build !linux

package image

import "fmt"

func dropMountedFilePageCache(string, string, int64, int64) error {
	return fmt.Errorf("page-cache eviction is supported only on Linux")
}
