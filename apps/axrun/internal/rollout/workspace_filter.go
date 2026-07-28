package rollout

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func uploadExcludePaths(statePath string, uploadPath string, excludePaths []string) []string {
	if len(excludePaths) == 0 {
		return nil
	}
	if filepath.Clean(statePath) == filepath.Clean(uploadPath) {
		return excludePaths
	}
	uploadRel, err := filepath.Rel(statePath, uploadPath)
	if err != nil || uploadRel == "." || uploadRel == ".." || strings.HasPrefix(uploadRel, "../") {
		return nil
	}
	uploadRel = filepath.ToSlash(uploadRel)
	var filtered []string
	for _, path := range excludePaths {
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == uploadRel {
			filtered = append(filtered, ".")
			continue
		}
		if strings.HasPrefix(path, uploadRel+"/") {
			filtered = append(filtered, strings.TrimPrefix(path, uploadRel+"/"))
		}
	}
	return filtered
}

func filteredUploadDir(source string, excludePaths []string) (string, func(), error) {
	target, err := os.MkdirTemp("", "axrun-upload-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create filtered upload directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(target) }
	if err := copyDirFiltered(source, target, excludePaths); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return target, cleanup, nil
}

func copyDirFiltered(source string, target string, excludePaths []string) error {
	excludes := cleanExcludePaths(excludePaths)
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if shouldExcludePath(rel, excludes) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink %s is not supported in filtered upload", path)
		}
		destination := filepath.Join(target, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(destination, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return copyFile(path, destination, info.Mode().Perm())
	})
}

func copyFile(source string, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
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

func cleanExcludePaths(paths []string) map[string]struct{} {
	excludes := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == "" {
			continue
		}
		excludes[path] = struct{}{}
	}
	return excludes
}

func shouldExcludePath(path string, excludes map[string]struct{}) bool {
	if _, ok := excludes["."]; ok {
		return true
	}
	if _, ok := excludes[path]; ok {
		return true
	}
	for exclude := range excludes {
		if strings.HasPrefix(path, exclude+"/") {
			return true
		}
	}
	return false
}
