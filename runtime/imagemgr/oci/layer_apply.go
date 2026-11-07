package oci

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type layerExtraction struct {
	SizeBytes           int64
	HasRootMetadata     bool
	ImplicitDirectories []string
	RequiresUserXattr   bool
}

func extractLayerTar(r io.Reader, dst string) (layerExtraction, error) {
	// The extraction root is guest-visible metadata, unlike its private host
	// ancestors. Layers without an explicit root entry use the OCI default.
	if err := os.Chmod(dst, 0755); err != nil {
		return layerExtraction{}, fmt.Errorf("set default layer root mode: %w", err)
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return layerExtraction{}, fmt.Errorf("open layer root: %w", err)
	}
	defer root.Close()
	tr := tar.NewReader(r)
	var totalSize int64
	var rootHeader *tar.Header
	requiresUserXattr := false
	implicit := make(map[string]struct{})

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return layerExtraction{}, fmt.Errorf("failed to read tar stream: %w", err)
		}
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		}

		relPath, err := cleanArchivePath(hdr.Name)
		if err != nil {
			return layerExtraction{}, err
		}
		if relPath == "" {
			if hdr.Typeflag != tar.TypeDir {
				return layerExtraction{}, fmt.Errorf("layer root entry must be a directory")
			}
			copy := *hdr
			rootHeader = &copy
			continue
		}

		if isWhiteout(relPath) {
			whiteout, opaque, err := whiteoutPath(relPath)
			if err != nil {
				return layerExtraction{}, err
			}
			if err := ensureLayerDirectory(root, filepath.Dir(relPath), implicit); err != nil {
				return layerExtraction{}, err
			}
			if !opaque {
				discardImplicitDirectories(implicit, whiteout)
			}
			usedUserXattr, err := applyOCIWhiteout(dst, relPath)
			if err != nil {
				return layerExtraction{}, err
			}
			requiresUserXattr = requiresUserXattr || usedUserXattr
			continue
		}

		target := filepath.Join(dst, relPath)
		if err := ensureLayerDirectory(root, filepath.Dir(relPath), implicit); err != nil {
			return layerExtraction{}, err
		}
		if hdr.Typeflag != tar.TypeDir {
			discardImplicitDirectories(implicit, relPath)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := ensureLayerDirectory(root, relPath, implicit); err != nil {
				return layerExtraction{}, fmt.Errorf("failed to create dir %s: %w", target, err)
			}
			delete(implicit, relPath)
		case tar.TypeReg, tar.TypeRegA:
			if err := os.RemoveAll(target); err != nil {
				return layerExtraction{}, fmt.Errorf("failed to replace file %s: %w", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, fileModeFromTar(hdr.Mode))
			if err != nil {
				return layerExtraction{}, fmt.Errorf("failed to create file %s: %w", target, err)
			}
			n, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			totalSize += n
			if copyErr != nil {
				return layerExtraction{}, fmt.Errorf("failed to write file %s: %w", target, copyErr)
			}
			if closeErr != nil {
				return layerExtraction{}, fmt.Errorf("failed to close file %s: %w", target, closeErr)
			}
		case tar.TypeSymlink:
			if err := os.RemoveAll(target); err != nil {
				return layerExtraction{}, fmt.Errorf("failed to replace symlink %s: %w", target, err)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return layerExtraction{}, fmt.Errorf("failed to create symlink %s -> %s: %w", target, hdr.Linkname, err)
			}
		case tar.TypeLink:
			linkTarget, err := cleanArchivePath(hdr.Linkname)
			if err != nil {
				return layerExtraction{}, fmt.Errorf("invalid hardlink target %s: %w", hdr.Linkname, err)
			}
			if linkTarget == "" {
				return layerExtraction{}, fmt.Errorf("invalid hardlink target %s", hdr.Linkname)
			}
			if err := os.RemoveAll(target); err != nil {
				return layerExtraction{}, fmt.Errorf("failed to replace hardlink %s: %w", target, err)
			}
			if err := root.Link(linkTarget, relPath); err != nil {
				return layerExtraction{}, fmt.Errorf("failed to create hardlink %s -> %s: %w", relPath, linkTarget, err)
			}
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			if err := os.RemoveAll(target); err != nil {
				return layerExtraction{}, fmt.Errorf("failed to replace special file %s: %w", target, err)
			}
			if err := createSpecialNode(target, hdr); err != nil {
				return layerExtraction{}, err
			}
		default:
			return layerExtraction{}, fmt.Errorf("unsupported tar entry type %d for %s", hdr.Typeflag, hdr.Name)
		}

		if err := applyMetadata(target, hdr); err != nil {
			return layerExtraction{}, err
		}
	}

	if rootHeader != nil {
		if err := applyMetadata(dst, rootHeader); err != nil {
			return layerExtraction{}, err
		}
	}
	paths := make([]string, 0, len(implicit))
	for name := range implicit {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	return layerExtraction{SizeBytes: totalSize, HasRootMetadata: rootHeader != nil, ImplicitDirectories: paths, RequiresUserXattr: requiresUserXattr}, nil
}

func createSpecialNode(target string, hdr *tar.Header) error {
	mode := uint32(hdr.Mode)
	switch hdr.Typeflag {
	case tar.TypeChar:
		mode |= syscall.S_IFCHR
	case tar.TypeBlock:
		mode |= syscall.S_IFBLK
	case tar.TypeFifo:
		mode |= syscall.S_IFIFO
	default:
		return nil
	}

	if err := syscall.Mknod(target, mode, 0); err != nil {
		// Non-root fallback: keep an empty regular file so extraction continues.
		f, createErr := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, fileModeFromTar(hdr.Mode))
		if createErr != nil {
			return fmt.Errorf("failed to create special node fallback %s after mknod failed (%v): %w", target, err, createErr)
		}
		_ = f.Close()
	}

	return nil
}

func applyMetadata(target string, hdr *tar.Header) error {
	if hdr.Typeflag == tar.TypeLink {
		return nil
	}
	if err := os.Lchown(target, hdr.Uid, hdr.Gid); err != nil {
		if !canIgnoreOwnershipError(err) {
			return fmt.Errorf("failed to set ownership for %s to %d:%d: %w", target, hdr.Uid, hdr.Gid, err)
		}
	}
	if hdr.Typeflag != tar.TypeSymlink {
		if err := os.Chtimes(target, hdr.AccessTime, hdr.ModTime); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to set mtime for %s: %w", target, err)
		}
		if err := os.Chmod(target, fileModeFromTar(hdr.Mode)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to set mode for %s: %w", target, err)
		}
	}
	return nil
}

func fileModeFromTar(mode int64) os.FileMode {
	fileMode := os.FileMode(mode).Perm()
	if mode&04000 != 0 {
		fileMode |= os.ModeSetuid
	}
	if mode&02000 != 0 {
		fileMode |= os.ModeSetgid
	}
	if mode&01000 != 0 {
		fileMode |= os.ModeSticky
	}
	return fileMode
}

func canIgnoreOwnershipError(err error) bool {
	if os.Geteuid() == 0 {
		return false
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		var errno syscall.Errno
		if errors.As(pathErr.Err, &errno) && (errno == syscall.EPERM || errno == syscall.EINVAL) {
			return true
		}
	}
	return false
}

func isWhiteout(relPath string) bool {
	base := filepath.Base(relPath)
	return strings.HasPrefix(base, ".wh.")
}

func applyOCIWhiteout(layerRoot string, relPath string) (bool, error) {
	relTarget, opaque, err := whiteoutPath(relPath)
	if err != nil {
		return false, err
	}
	root, err := os.OpenRoot(layerRoot)
	if err != nil {
		return false, err
	}
	defer root.Close()
	dir := filepath.Dir(relPath)
	if err := ensureLayerDirectory(root, dir, make(map[string]struct{})); err != nil {
		return false, err
	}
	parent, err := root.Open(dir)
	if err != nil {
		return false, err
	}
	defer parent.Close()
	if opaque {
		return setOverlayXattr(parent, "opaque", []byte("y"))
	}
	if err := root.RemoveAll(relTarget); err != nil {
		return false, fmt.Errorf("remove whiteout target: %w", err)
	}
	// Parent traversal is confined and rejects symlinks before the only operation
	// lacking an os.Root equivalent. The private extraction tree has one writer.
	target := filepath.Join(layerRoot, relTarget)
	if err := syscall.Mknod(target, syscall.S_IFCHR|0600, 0); err == nil {
		return false, nil
	}
	file, err := root.OpenFile(relTarget, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		return false, fmt.Errorf("create whiteout marker: %w", err)
	}
	usedUserXattr, metadataErr := setOverlayXattr(file, "whiteout", []byte("y"))
	closeErr := file.Close()
	return usedUserXattr, errors.Join(metadataErr, closeErr)
}

func whiteoutPath(relPath string) (string, bool, error) {
	if !filepath.IsLocal(relPath) || filepath.Clean(relPath) != relPath || strings.IndexByte(relPath, 0) >= 0 {
		return "", false, fmt.Errorf("invalid OCI whiteout path %q", relPath)
	}
	base := filepath.Base(relPath)
	if base == ".wh..wh..opq" {
		return filepath.Dir(relPath), true, nil
	}
	name := strings.TrimPrefix(base, ".wh.")
	if name == base || name == "" || name == "." || name == ".." {
		return "", false, fmt.Errorf("invalid OCI whiteout name %q", relPath)
	}
	return filepath.Join(filepath.Dir(relPath), name), false, nil
}

func setOverlayXattr(target *os.File, key string, value []byte) (bool, error) {
	trustedKey := fmt.Sprintf("trusted.overlay.%s", key)
	if err := unix.Fsetxattr(int(target.Fd()), trustedKey, value, 0); err == nil {
		return false, nil
	}
	userKey := fmt.Sprintf("user.overlay.%s", key)
	if err := unix.Fsetxattr(int(target.Fd()), userKey, value, 0); err == nil {
		return true, nil
	}
	return false, fmt.Errorf("unable to set overlay xattr %q", key)
}

func cleanArchivePath(raw string) (string, error) {
	cleaned := path.Clean("/" + raw)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." {
		return "", nil
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("invalid archive path %q", raw)
	}
	return cleaned, nil
}

func ensureLayerDirectory(root *os.Root, name string, implicit map[string]struct{}) error {
	if name == "." || name == "" {
		return nil
	}
	if err := ensureLayerDirectory(root, filepath.Dir(name), implicit); err != nil {
		return err
	}
	if info, err := root.Lstat(name); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("layer parent %s is not a directory", name)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect layer parent %s: %w", name, err)
	}
	if err := root.Mkdir(name, 0755); err != nil {
		return fmt.Errorf("create layer parent %s: %w", name, err)
	}
	// Only newly created implicit parents get a default mode. Existing
	// directories retain their explicit archive metadata.
	if err := root.Chmod(name, 0755); err != nil {
		return fmt.Errorf("set layer parent mode %s: %w", name, err)
	}
	implicit[name] = struct{}{}
	return nil
}

func discardImplicitDirectories(implicit map[string]struct{}, target string) {
	for name := range implicit {
		if name == target || strings.HasPrefix(name, target+string(filepath.Separator)) {
			delete(implicit, name)
		}
	}
}
