package oci

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

type layerExtraction struct {
	SizeBytes       int64
	HasRootMetadata bool
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

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return layerExtraction{}, fmt.Errorf("failed to read tar stream: %w", err)
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
			if err := ensureLayerDirectory(root, filepath.Dir(relPath)); err != nil {
				return layerExtraction{}, err
			}
			if err := applyOCIWhiteout(dst, relPath); err != nil {
				return layerExtraction{}, err
			}
			continue
		}

		target := filepath.Join(dst, relPath)
		if err := ensureLayerDirectory(root, filepath.Dir(relPath)); err != nil {
			return layerExtraction{}, err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := ensureLayerDirectory(root, relPath); err != nil {
				return layerExtraction{}, fmt.Errorf("failed to create dir %s: %w", target, err)
			}
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
		case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
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
	return layerExtraction{SizeBytes: totalSize, HasRootMetadata: rootHeader != nil}, nil
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

func applyOCIWhiteout(layerRoot string, relPath string) error {
	dir := filepath.Dir(relPath)
	if dir == "." {
		dir = ""
	}
	base := filepath.Base(relPath)
	parent := filepath.Join(layerRoot, dir)

	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("failed to ensure whiteout parent dir %s: %w", parent, err)
	}

	if base == ".wh..wh..opq" {
		if err := setOverlayXattr(parent, "opaque", []byte("y")); err != nil {
			logrus.Warnf("failed to set opaque xattr on %s: %v", parent, err)
		}
		return nil
	}

	whiteoutTarget := filepath.Join(parent, strings.TrimPrefix(base, ".wh."))
	if err := os.RemoveAll(whiteoutTarget); err != nil {
		return fmt.Errorf("failed to cleanup whiteout target %s: %w", whiteoutTarget, err)
	}

	if err := syscall.Mknod(whiteoutTarget, syscall.S_IFCHR|0600, 0); err == nil {
		return nil
	}

	// Non-root fallback: regular file plus overlay whiteout xattr when possible.
	f, err := os.OpenFile(whiteoutTarget, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create whiteout file %s: %w", whiteoutTarget, err)
	}
	_ = f.Close()
	if err := setOverlayXattr(whiteoutTarget, "whiteout", []byte("y")); err != nil {
		logrus.Warnf("failed to set whiteout xattr on %s: %v", whiteoutTarget, err)
	}

	return nil
}

func setOverlayXattr(target, key string, value []byte) error {
	trustedKey := fmt.Sprintf("trusted.overlay.%s", key)
	if err := unix.Setxattr(target, trustedKey, value, 0); err == nil {
		return nil
	}
	userKey := fmt.Sprintf("user.overlay.%s", key)
	if err := unix.Setxattr(target, userKey, value, 0); err == nil {
		return nil
	}
	return fmt.Errorf("unable to set overlay xattr %q", key)
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

func ensureLayerDirectory(root *os.Root, name string) error {
	if name == "." || name == "" {
		return nil
	}
	if info, err := root.Stat(name); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("layer parent %s is not a directory", name)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect layer parent %s: %w", name, err)
	}
	if err := ensureLayerDirectory(root, filepath.Dir(name)); err != nil {
		return err
	}
	if err := root.Mkdir(name, 0755); err != nil {
		return fmt.Errorf("create layer parent %s: %w", name, err)
	}
	// Only newly created implicit parents get a default mode. Existing
	// directories retain their explicit archive metadata.
	if err := root.Chmod(name, 0755); err != nil {
		return fmt.Errorf("set layer parent mode %s: %w", name, err)
	}
	return nil
}
