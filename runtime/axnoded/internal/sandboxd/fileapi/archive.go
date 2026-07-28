package fileapi

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
)

const (
	maxArchiveEntries    = 4096
	maxArchiveBytes      = 256 << 20
	maxArchiveEntryBytes = 64 << 20
	maxArchivePathDepth  = 64
)

type LimitSnapshot struct {
	MaxArchiveEntries    int
	MaxArchiveBytes      int64
	MaxArchiveEntryBytes int64
	MaxArchivePathDepth  int
}

func Limits() LimitSnapshot {
	return LimitSnapshot{
		MaxArchiveEntries:    maxArchiveEntries,
		MaxArchiveBytes:      maxArchiveBytes,
		MaxArchiveEntryBytes: maxArchiveEntryBytes,
		MaxArchivePathDepth:  maxArchivePathDepth,
	}
}

func (s *Service) UploadArchive(options UploadArchiveOptions, input io.Reader) error {
	if err := validateArchiveOptions("upload archive", options.Path, options.Format, options.SymlinkPolicy); err != nil {
		return err
	}
	data, err := sanitizedArchive(input)
	if err != nil {
		return err
	}
	if err := prepareArchiveTarget(options.Path, options.CreateParents, options.Overwrite); err != nil {
		return err
	}
	return extractArchive(options.Path, bytes.NewReader(data))
}

func (s *Service) DownloadArchive(options DownloadArchiveOptions, output io.Writer) error {
	if err := validateArchiveOptions("download archive", options.Path, options.Format, options.SymlinkPolicy); err != nil {
		return err
	}
	if err := validateArchiveSource(options.Path); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := writeArchive(options.Path, &buf); err != nil {
		return err
	}
	data, err := sanitizedArchive(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}
	_, err = io.Copy(output, bytes.NewReader(data))
	return err
}

func validateArchiveOptions(operation string, sandboxPath string, format filev1.SandboxArchiveFormat, policy filev1.SandboxArchiveSymlinkPolicy) error {
	if sandboxPath == "" {
		return fmt.Errorf("%s: path is required: %w", operation, errord.ErrInvalidArgument)
	}
	if format != filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR {
		return fmt.Errorf("%s %s: only tar archives are supported: %w", operation, sandboxPath, errord.ErrInvalidArgument)
	}
	if policy != filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT {
		return fmt.Errorf("%s %s: only reject symlink policy is supported: %w", operation, sandboxPath, errord.ErrInvalidArgument)
	}
	return nil
}

func prepareArchiveTarget(path string, createParents, overwrite bool) error {
	if err := rejectSymlinkAncestors(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return classifyPathError("upload archive", path, err)
		}
		if !createParents {
			return fmt.Errorf("upload archive %s: %w", path, errord.ErrNotFound)
		}
		if err := os.MkdirAll(path, 0755); err != nil {
			return classifyPathError("upload archive", path, err)
		}
		return nil
	}
	if !overwrite {
		return fmt.Errorf("upload archive %s: path already exists: %w", path, errord.ErrAlreadyExists)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("upload archive %s: target is not a directory: %w", path, errord.ErrFailedPrecondition)
	}
	return rejectSymlinksInTree(path)
}

func validateArchiveSource(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return classifyPathError("download archive", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("download archive %s: target is not a directory: %w", path, errord.ErrFailedPrecondition)
	}
	return rejectSymlinksInTree(path)
}

func rejectSymlinkAncestors(path string) error {
	current := filepath.Clean(path)
	for {
		parent := filepath.Dir(current)
		if parent == current || parent == "." {
			return nil
		}
		info, err := os.Lstat(parent)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive target parent is a symlink: %s: %w", parent, errord.ErrFailedPrecondition)
		}
		current = parent
	}
}

func rejectSymlinksInTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return classifyPathError("archive", path, err)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive target contains symlink: %s: %w", path, errord.ErrFailedPrecondition)
		}
		return nil
	})
}

func sanitizedArchive(input io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tr := tar.NewReader(input)
	var entries int
	var totalBytes int64
	for {
		header, readErr := tr.Next()
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, fmt.Errorf("read tar archive: %w", errord.ErrInvalidArgument)
		}
		if err := validateArchiveHeader(header); err != nil {
			return nil, err
		}
		entries++
		if entries > maxArchiveEntries {
			return nil, fmt.Errorf("archive contains too many entries: %w", errord.ErrFailedPrecondition)
		}
		if header.Size > maxArchiveEntryBytes {
			return nil, fmt.Errorf("archive entry %q exceeds maximum size: %w", header.Name, errord.ErrFailedPrecondition)
		}
		cloned := *header
		if err := tw.WriteHeader(&cloned); err != nil {
			return nil, fmt.Errorf("write sanitized tar header: %w", errord.ErrFailedPrecondition)
		}
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			written, err := io.Copy(tw, tr)
			if err != nil {
				return nil, fmt.Errorf("write sanitized tar content: %w", errord.ErrFailedPrecondition)
			}
			totalBytes += written
			if totalBytes > maxArchiveBytes {
				return nil, fmt.Errorf("archive exceeds maximum total size: %w", errord.ErrFailedPrecondition)
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("finish sanitized tar: %w", errord.ErrFailedPrecondition)
	}
	return buf.Bytes(), nil
}

func validateArchiveHeader(header *tar.Header) error {
	name := header.Name
	if name == "" {
		return fmt.Errorf("archive entry path is required: %w", errord.ErrInvalidArgument)
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("archive entry %q is absolute: %w", name, errord.ErrInvalidArgument)
	}
	parts := strings.Split(name, "/")
	if len(parts) > maxArchivePathDepth {
		return fmt.Errorf("archive entry %q exceeds maximum path depth: %w", name, errord.ErrInvalidArgument)
	}
	for _, part := range parts {
		if part == ".." {
			return fmt.Errorf("archive entry %q escapes target directory: %w", name, errord.ErrInvalidArgument)
		}
	}
	if filepath.Clean(name) == ".." {
		return fmt.Errorf("archive entry %q escapes target directory: %w", name, errord.ErrInvalidArgument)
	}
	switch header.Typeflag {
	case tar.TypeDir, tar.TypeReg, tar.TypeRegA:
		if header.Size < 0 {
			return fmt.Errorf("archive entry %q has invalid size: %w", name, errord.ErrInvalidArgument)
		}
		return nil
	case tar.TypeSymlink, tar.TypeLink:
		return fmt.Errorf("archive entry %q is a link: %w", name, errord.ErrInvalidArgument)
	default:
		return fmt.Errorf("archive entry %q has unsupported type %d: %w", name, header.Typeflag, errord.ErrInvalidArgument)
	}
}

func extractArchive(target string, input io.Reader) error {
	tr := tar.NewReader(input)
	for {
		header, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read tar archive: %w", errord.ErrInvalidArgument)
		}
		if err := validateArchiveHeader(header); err != nil {
			return err
		}
		dst := filepath.Join(target, filepath.Clean(header.Name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, os.FileMode(header.Mode).Perm()); err != nil {
				return classifyPathError("upload archive", dst, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				return classifyPathError("upload archive", dst, err)
			}
			file, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode).Perm())
			if err != nil {
				return classifyPathError("upload archive", dst, err)
			}
			_, copyErr := io.Copy(file, tr)
			closeErr := file.Close()
			if copyErr != nil {
				_ = os.Remove(dst)
				return classifyPathError("upload archive", dst, copyErr)
			}
			if closeErr != nil {
				_ = os.Remove(dst)
				return classifyPathError("upload archive", dst, closeErr)
			}
		}
	}
}

func writeArchive(root string, output io.Writer) error {
	tw := tar.NewWriter(output)
	defer tw.Close()
	var entries int
	var totalBytes int64
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return classifyPathError("download archive", path, err)
		}
		info, err := entry.Info()
		if err != nil {
			return classifyPathError("download archive", path, err)
		}
		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("download archive %s: too many entries: %w", root, errord.ErrFailedPrecondition)
		}
		if info.Mode().IsRegular() {
			if info.Size() > maxArchiveEntryBytes {
				return fmt.Errorf("download archive %s: file %s exceeds maximum size: %w", root, path, errord.ErrFailedPrecondition)
			}
			totalBytes += info.Size()
			if totalBytes > maxArchiveBytes {
				return fmt.Errorf("download archive %s: exceeds maximum total size: %w", root, errord.ErrFailedPrecondition)
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return classifyPathError("download archive", path, err)
		}
		name := "."
		if rel != "." {
			name = filepath.ToSlash(rel)
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("create tar header for %s: %w", path, errord.ErrFailedPrecondition)
		}
		header.Name = name
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("write tar header for %s: %w", path, errord.ErrFailedPrecondition)
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return classifyPathError("download archive", path, err)
			}
			_, copyErr := io.Copy(tw, file)
			closeErr := file.Close()
			if copyErr != nil {
				return classifyPathError("download archive", path, copyErr)
			}
			if closeErr != nil {
				return classifyPathError("download archive", path, closeErr)
			}
		}
		return nil
	})
}
