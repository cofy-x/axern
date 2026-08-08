//go:build !linux

package rootfsview

import "fmt"

func applyProjectQuota(string, string, uint32, int64) error {
	return fmt.Errorf("runc writable rootfs project quota requires Linux XFS")
}
