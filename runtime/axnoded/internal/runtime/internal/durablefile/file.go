package durablefile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write atomically replaces target and durably records both file contents and
// the directory entry. The caller owns creation and permissions of the parent.
func Write(target string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(target)
	temporary, err := os.CreateTemp(dir, ".axern-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return err
	}
	if err := SyncDir(dir); err != nil {
		return fmt.Errorf("sync parent directory: %w", err)
	}
	return nil
}

func SyncDir(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
