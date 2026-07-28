package fileapi

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func copyFile(srcPath, dstPath string, mode os.FileMode, overwrite bool) error {
	flag := os.O_WRONLY | os.O_CREATE
	if overwrite {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_EXCL
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return classifyPathError("copy", srcPath, err)
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return classifyPathError("copy", dstPath, err)
	}
	dst, err := os.OpenFile(dstPath, flag, mode)
	if err != nil {
		return classifyPathError("copy", dstPath, err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		return classifyPathError("copy", dstPath, copyErr)
	}
	if closeErr != nil {
		return classifyPathError("copy", dstPath, closeErr)
	}
	return nil
}

func copyDir(srcPath, dstPath string, overwrite bool) error {
	return filepath.WalkDir(srcPath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return classifyPathError("copy", path, err)
		}
		rel, err := filepath.Rel(srcPath, path)
		if err != nil {
			return classifyPathError("copy", path, err)
		}
		target := filepath.Join(dstPath, rel)
		info, err := entry.Info()
		if err != nil {
			return classifyPathError("copy", path, err)
		}
		switch {
		case entry.IsDir():
			if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
				return classifyPathError("copy", target, err)
			}
		case entry.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return classifyPathError("copy", path, err)
			}
			if _, err := os.Lstat(target); err == nil {
				if !overwrite {
					return fmt.Errorf("copy %s to %s: path already exists: %w", path, target, errord.ErrAlreadyExists)
				}
				if err := os.RemoveAll(target); err != nil {
					return classifyPathError("copy", target, err)
				}
			}
			if err := os.Symlink(link, target); err != nil {
				return classifyPathError("copy", target, err)
			}
		default:
			if err := copyFile(path, target, info.Mode().Perm(), overwrite); err != nil {
				return err
			}
		}
		return nil
	})
}
