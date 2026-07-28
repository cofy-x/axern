package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func (i instance) UploadDir(_ context.Context, localPath string, remotePath string, options sandbox.UploadDirOptions) error {
	if strings.TrimSpace(localPath) == "" || strings.TrimSpace(remotePath) == "" {
		return fmt.Errorf("local_path and remote_path are required")
	}
	target := i.mapPath(remotePath)
	if !options.NoCreateParents {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
	}
	if options.NoOverwrite {
		empty, err := missingOrEmpty(target)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("remote path %s already exists and is not empty", remotePath)
		}
	}
	if err := copyDirContents(localPath, target); err != nil {
		return err
	}
	if options.Writable {
		return makeTreeWritable(target)
	}
	return nil
}

func (i instance) DownloadPath(_ context.Context, remotePath string, localPath string, options sandbox.DownloadPathOptions) error {
	if strings.TrimSpace(remotePath) == "" || strings.TrimSpace(localPath) == "" {
		return fmt.Errorf("remote_path and local_path are required")
	}
	if options.NoOverwrite {
		empty, err := missingOrEmpty(localPath)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("local path %s already exists and is not empty", localPath)
		}
	} else {
		_ = os.RemoveAll(localPath)
	}
	source := i.mapPath(remotePath)
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink %s is not supported", remotePath)
	}
	if info.IsDir() {
		if err := os.MkdirAll(localPath, 0o755); err != nil {
			return err
		}
		return copyDirContents(source, localPath)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("remote path %s is not a regular file or directory", remotePath)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	return copyFile(source, localPath, info.Mode().Perm())
}

func copyDirContents(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink %s is not supported", path)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src string, dst string, mode os.FileMode) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func missingOrEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func makeTreeWritable(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() | 0o666
		if entry.IsDir() || info.Mode().Perm()&0o111 != 0 {
			mode |= 0o111
		}
		return os.Chmod(path, mode)
	})
}
