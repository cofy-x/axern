//go:build !linux

package allocation

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func copyTaskAsset(source, workspaceRoot, destinationRelative string) error {
	destination := filepath.Join(workspaceRoot, destinationRelative)
	sourceRoot := source
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("unsupported task asset file type at %s", current)
		}
		rel, err := filepath.Rel(sourceRoot, current)
		if err != nil {
			return err
		}
		outputPath := destination
		if rel != "." {
			outputPath = filepath.Join(destination, rel)
		}
		if info.IsDir() {
			return os.MkdirAll(outputPath, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		in, err := os.Open(current)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		inErr, outErr := in.Close(), out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		return outErr
	})
}
