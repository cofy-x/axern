//go:build !linux

package inspect

import (
	"errors"
	"os"
	"path/filepath"
)

func inspectLink(pinPath, name string) ObjectInfo {
	path := filepath.Join(pinPath, "links", name)
	info := ObjectInfo{Kind: "link", Name: name, Path: path}
	if _, err := os.Stat(path); err == nil {
		info.Present = true
		info.Error = "opening pinned links is only supported on Linux"
	} else if !errors.Is(err, os.ErrNotExist) {
		info.Error = err.Error()
	}
	return info
}
