package fileutil

import (
	"os"
	"path/filepath"
)

func AtomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(filename), ".tmp-"+filepath.Base(filename))
	if err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}
