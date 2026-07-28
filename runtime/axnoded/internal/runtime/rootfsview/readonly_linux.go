//go:build linux

package rootfsview

import "github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"

func rootfsPathReadOnly(rootDir string) (bool, error) {
	return hostlinux.IsPathReadOnly(rootDir)
}
