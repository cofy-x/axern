//go:build !linux

package serverruntime

import "os"

func rootOwned(os.FileInfo) bool { return false }
