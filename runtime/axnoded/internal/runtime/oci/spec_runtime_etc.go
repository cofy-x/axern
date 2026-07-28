package oci

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	spec "github.com/opencontainers/runtime-spec/specs-go"
)

type runtimeEtcFile struct {
	name    string
	target  string
	content string
}

func managedRuntimeEtcFiles(ociSpec *spec.Spec, files []runtimeEtcFile) []runtimeEtcFile {
	managed := files[:0]
	for _, file := range files {
		if hasMountDestination(ociSpec, file.target) {
			continue
		}
		managed = append(managed, file)
	}
	return managed
}

func runtimeRootfsPath(bundleDir string, ociSpec *spec.Spec) string {
	if ociSpec == nil || ociSpec.Root == nil || strings.TrimSpace(ociSpec.Root.Path) == "" {
		return ""
	}
	rootPath := strings.TrimSpace(ociSpec.Root.Path)
	if filepath.IsAbs(rootPath) {
		return rootPath
	}
	return filepath.Join(bundleDir, rootPath)
}

func shouldUseRuntimeEtcDirMount(ociSpec *spec.Spec, rootfsPath string, files []runtimeEtcFile) bool {
	if rootfsPath == "" || ociSpec == nil || ociSpec.Root == nil || !ociSpec.Root.Readonly || hasMountDestination(ociSpec, "/etc") {
		return false
	}
	if info, err := os.Stat(filepath.Join(rootfsPath, "etc")); err != nil || !info.IsDir() {
		return false
	}
	for _, file := range files {
		if _, err := os.Lstat(filepath.Join(rootfsPath, strings.TrimPrefix(file.target, "/"))); os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func materializeRuntimeEtcDir(rootfsPath, managedEtcDir string, files []runtimeEtcFile) error {
	if err := os.RemoveAll(managedEtcDir); err != nil {
		return err
	}
	if err := copyRuntimeEtcDir(filepath.Join(rootfsPath, "etc"), managedEtcDir); err != nil {
		return err
	}
	for _, file := range files {
		path := filepath.Join(managedEtcDir, file.name)
		if err := writeManagedRuntimeEtcFile(path, file.content); err != nil {
			return err
		}
	}
	return nil
}

func copyRuntimeEtcDir(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(target, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, targetPath)
		case entry.IsDir():
			return os.MkdirAll(targetPath, info.Mode().Perm())
		case entry.Type().IsRegular():
			return copyRegularFile(path, targetPath, info.Mode().Perm())
		default:
			return nil
		}
	})
}

func copyRegularFile(source, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func writeManagedRuntimeEtcFile(path, content string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func insertRuntimeEtcDirMount(ociSpec *spec.Spec, source string) {
	mount := spec.Mount{
		Destination: "/etc",
		Type:        "bind",
		Source:      source,
		Options:     []string{"rbind", "ro"},
	}
	insertAt := len(ociSpec.Mounts)
	for i, existing := range ociSpec.Mounts {
		if existing.Destination == "/etc" || strings.HasPrefix(existing.Destination, "/etc/") {
			insertAt = i
			break
		}
	}
	ociSpec.Mounts = append(ociSpec.Mounts, spec.Mount{})
	copy(ociSpec.Mounts[insertAt+1:], ociSpec.Mounts[insertAt:])
	ociSpec.Mounts[insertAt] = mount
}
