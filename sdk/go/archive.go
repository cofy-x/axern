package axernsdk

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// UploadDirOptions configures directory uploads into a sandbox.
type UploadDirOptions struct {
	NoCreateParents bool
	NoOverwrite     bool
}

// DownloadDirOptions configures directory downloads from a sandbox.
type DownloadDirOptions struct {
	NoOverwrite bool
}

// UploadDir uploads the contents of localPath into remotePath.
func (s *Sandbox) UploadDir(ctx context.Context, localPath, remotePath string, options UploadDirOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.UploadDir(ctx, localPath, remotePath, options)
}

// DownloadDir downloads the contents of remotePath into localPath.
func (s *Sandbox) DownloadDir(ctx context.Context, remotePath, localPath string, options DownloadDirOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.DownloadDir(ctx, remotePath, localPath, options)
}

func (n *NodeSandboxClient) UploadDir(ctx context.Context, localPath, remotePath string, options UploadDirOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if localPath == "" || remotePath == "" {
		return &PathError{Message: "local_path and remote_path are required"}
	}
	err := n.rpcClient().UploadArchive(ctx, remotePath, func() (io.Reader, func() error, error) {
		reader, writer := io.Pipe()
		errCh := make(chan error, 1)
		go func() {
			errCh <- writeDirectoryTar(writer, localPath)
		}()
		closeReader := func() error {
			closeErr := reader.Close()
			tarErr := <-errCh
			if closeErr != nil {
				return closeErr
			}
			return tarErr
		}
		return reader, closeReader, nil
	}, !options.NoCreateParents, !options.NoOverwrite)
	if err != nil {
		return mapRPCError(err, "sandbox upload directory", n.allocationID)
	}
	return nil
}

func (n *NodeSandboxClient) DownloadDir(ctx context.Context, remotePath, localPath string, options DownloadDirOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if remotePath == "" || localPath == "" {
		return &PathError{Message: "remote_path and local_path are required"}
	}
	if options.NoOverwrite {
		empty, err := localPathMissingOrEmpty(localPath)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("local path %s already exists and is not empty", localPath)
		}
	}
	tmp, err := os.CreateTemp("", "axern-go-sdk-download-*.tar")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	defer tmp.Close()
	downloadErr := n.rpcClient().DownloadArchive(ctx, remotePath, func() (io.Writer, func(error) error, error) {
		if err := tmp.Truncate(0); err != nil {
			return nil, nil, err
		}
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return nil, nil, err
		}
		return tmp, func(error) error { return nil }, nil
	})
	if downloadErr != nil {
		return mapRPCError(downloadErr, "sandbox download directory", n.allocationID)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return extractSafeTar(localPath, tmp, !options.NoOverwrite)
}

func writeDirectoryTar(writer *io.PipeWriter, root string) error {
	defer writer.Close()
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("local path %s is not a directory", root)
	}
	tw := tar.NewWriter(writer)
	defer tw.Close()
	return filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("local symlink %s is not supported", filePath)
		}
		if filePath == root {
			return nil
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if entry.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func extractSafeTar(root string, reader io.Reader, overwrite bool) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if path.Clean(header.Name) == "." {
			if header.Typeflag == tar.TypeDir {
				continue
			}
			return fmt.Errorf("archive entry %s escapes target directory", header.Name)
		}
		target, err := safeTarTarget(rootAbs, header.Name)
		if err != nil {
			return err
		}
		if header.FileInfo().Mode()&os.ModeSymlink != 0 || header.Typeflag == tar.TypeLink || header.Typeflag == tar.TypeSymlink {
			return fmt.Errorf("archive entry %s uses unsupported link type", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureNoLocalSymlinkPath(rootAbs, target); err != nil {
				return err
			}
			if err := os.MkdirAll(target, os.FileMode(header.Mode)&0o777); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if !overwrite {
				if _, err := os.Lstat(target); err == nil {
					return fmt.Errorf("local path %s already exists", target)
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			parent := filepath.Dir(target)
			if err := ensureNoLocalSymlinkPath(rootAbs, parent); err != nil {
				return err
			}
			if err := os.MkdirAll(parent, 0o755); err != nil {
				return err
			}
			if err := ensureNoLocalSymlink(target); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, tr)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("archive entry %s has unsupported type %d", header.Name, header.Typeflag)
		}
	}
}

func safeTarTarget(rootAbs, name string) (string, error) {
	if name == "" {
		return "", errors.New("archive entry path is empty")
	}
	clean := path.Clean(name)
	if path.IsAbs(name) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("archive entry %s escapes target directory", name)
	}
	target := filepath.Join(rootAbs, filepath.FromSlash(clean))
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %s escapes target directory", name)
	}
	return targetAbs, nil
}

func ensureNoLocalSymlink(target string) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("local path %s is a symlink", target)
	}
	return nil
}

func ensureNoLocalSymlinkPath(rootAbs, targetAbs string) error {
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return err
	}
	if rel == "." {
		return ensureNoLocalSymlink(rootAbs)
	}
	current := rootAbs
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := ensureNoLocalSymlink(current); err != nil {
			return err
		}
	}
	return nil
}

func localPathMissingOrEmpty(localPath string) (bool, error) {
	info, err := os.Lstat(localPath)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("local path %s is a symlink", localPath)
	}
	if !info.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(localPath)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
