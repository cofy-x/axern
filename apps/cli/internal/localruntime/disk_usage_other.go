//go:build !darwin && !linux

package localruntime

import "os"

type allocatedFileIdentity struct{}

func allocatedFileUsage(info os.FileInfo) (int64, allocatedFileIdentity, bool) {
	return info.Size(), allocatedFileIdentity{}, false
}
