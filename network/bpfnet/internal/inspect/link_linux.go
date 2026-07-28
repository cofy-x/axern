//go:build linux

package inspect

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/cilium/ebpf/link"
)

func inspectLink(pinPath, name string) ObjectInfo {
	path := filepath.Join(pinPath, "links", name)
	info := ObjectInfo{Kind: "link", Name: name, Path: path}
	if _, err := os.Stat(path); err == nil {
		info.Present = true
	} else if !errors.Is(err, os.ErrNotExist) {
		info.Error = err.Error()
	}

	pinned, err := link.LoadPinnedLink(path, nil)
	if err != nil {
		if info.Error == "" && !errors.Is(err, os.ErrNotExist) {
			info.Error = err.Error()
		}
		return info
	}
	defer pinned.Close()
	info.Openable = true
	return info
}
