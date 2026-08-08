package oci

import (
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/durablefile"
)

func atomicWriteFile(target string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return durablefile.Write(target, content, mode)
}
