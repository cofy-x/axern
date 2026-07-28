package rootfssupport

import (
	"os"
	"path/filepath"
)

type Dir struct {
	Path string
	Mode os.FileMode
}

var dirs = []Dir{
	{Path: "proc", Mode: 0755},
	{Path: "dev", Mode: 0755},
	{Path: "sys", Mode: 0755},
	{Path: "tmp", Mode: 01777},
	{Path: "run", Mode: 0755},
	{Path: "mnt", Mode: 0755},
	{Path: filepath.Join("var", "run"), Mode: 0755},
}

func Dirs() []Dir {
	out := make([]Dir, len(dirs))
	copy(out, dirs)
	return out
}

func Ensure(root string) error {
	if root == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	for _, entry := range dirs {
		path := filepath.Join(root, entry.Path)
		if err := os.MkdirAll(path, entry.Mode); err != nil {
			return err
		}
		if err := os.Chmod(path, entry.Mode); err != nil {
			return err
		}
	}
	return nil
}
