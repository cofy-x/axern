//go:build !linux

package service

import "os"

// Production conformance runs on Linux. Other hosts retain a conservative
// cache for unit tests and local tooling; replacement, size, mode, or mtime
// changes always invalidate it.
func runtimeFileMetadataEqual(left, right os.FileInfo) bool {
	return os.SameFile(left, right) && left.Size() == right.Size() && left.Mode() == right.Mode() && left.ModTime().Equal(right.ModTime())
}
