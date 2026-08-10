//go:build !linux

package rootfsview

import "fmt"

func applyProjectQuota(string, string, uint32, int64) error {
	return fmt.Errorf("runc writable rootfs project quota requires Linux XFS")
}

func clearProjectQuota(string, string, uint32) error {
	return fmt.Errorf("runc writable rootfs project quota cleanup requires Linux XFS")
}

func VerifyProjectQuota(string, string, uint32, int64) error {
	return fmt.Errorf("XFS project quota verification requires Linux")
}
