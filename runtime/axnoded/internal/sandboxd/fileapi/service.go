package fileapi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Stat(path string) (StatResponse, error) {
	info, err := fileInfo(path)
	if err != nil {
		return StatResponse{}, err
	}
	return StatResponse{Info: info}, nil
}

func (s *Service) List(path string) (ListResponse, error) {
	if strings.TrimSpace(path) == "" {
		return ListResponse{}, fmt.Errorf("path is required: %w", errord.ErrInvalidArgument)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return ListResponse{}, classifyPathError("list directory", path, err)
	}
	infos := make([]*filev1.SandboxFileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := fileInfo(filepath.Join(path, entry.Name()))
		if err != nil {
			return ListResponse{}, err
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Path < infos[j].Path })
	return ListResponse{Entries: infos}, nil
}

func (s *Service) Read(path string) (ReadResponse, error) {
	if strings.TrimSpace(path) == "" {
		return ReadResponse{}, fmt.Errorf("path is required: %w", errord.ErrInvalidArgument)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return ReadResponse{}, classifyPathError("read file", path, err)
	}
	if info.IsDir() {
		return ReadResponse{}, fmt.Errorf("read file %s: path is a directory: %w", path, errord.ErrFailedPrecondition)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ReadResponse{}, classifyPathError("read file", path, err)
	}
	return ReadResponse{Data: data}, nil
}

func (s *Service) Exists(path string) (ExistsResponse, error) {
	if strings.TrimSpace(path) == "" {
		return ExistsResponse{}, fmt.Errorf("path is required: %w", errord.ErrInvalidArgument)
	}
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return ExistsResponse{Exists: true}, nil
	case errors.Is(err, os.ErrNotExist):
		return ExistsResponse{Exists: false}, nil
	default:
		return ExistsResponse{}, classifyPathError("exists", path, err)
	}
}

func (s *Service) Write(request WriteRequest) error {
	if strings.TrimSpace(request.Path) == "" {
		return fmt.Errorf("path is required: %w", errord.ErrInvalidArgument)
	}
	if request.CreateParents {
		if err := os.MkdirAll(filepath.Dir(request.Path), 0755); err != nil {
			return classifyPathError("write file", request.Path, err)
		}
	}
	if err := os.WriteFile(request.Path, request.Data, 0644); err != nil {
		return classifyPathError("write file", request.Path, err)
	}
	return nil
}

func (s *Service) Mkdir(request MkdirRequest) error {
	if strings.TrimSpace(request.Path) == "" {
		return fmt.Errorf("path is required: %w", errord.ErrInvalidArgument)
	}
	var err error
	if request.Parents {
		err = os.MkdirAll(request.Path, 0755)
	} else {
		err = os.Mkdir(request.Path, 0755)
	}
	if err != nil {
		return classifyPathError("make directory", request.Path, err)
	}
	return nil
}

func (s *Service) Remove(request RemoveRequest) error {
	if strings.TrimSpace(request.Path) == "" {
		return fmt.Errorf("path is required: %w", errord.ErrInvalidArgument)
	}
	var err error
	if request.Recursive {
		if !request.Force {
			if _, statErr := os.Lstat(request.Path); statErr != nil {
				return classifyPathError("remove", request.Path, statErr)
			}
		}
		err = os.RemoveAll(request.Path)
	} else {
		err = os.Remove(request.Path)
	}
	if err != nil {
		if request.Force && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return classifyPathError("remove", request.Path, err)
	}
	return nil
}

func (s *Service) Copy(request CopyRequest) error {
	if strings.TrimSpace(request.SrcPath) == "" || strings.TrimSpace(request.DstPath) == "" {
		return fmt.Errorf("src_path and dst_path are required: %w", errord.ErrInvalidArgument)
	}
	src, err := os.Lstat(request.SrcPath)
	if err != nil {
		return classifyPathError("copy", request.SrcPath, err)
	}
	if _, err := os.Lstat(request.DstPath); err == nil && !request.Overwrite {
		return fmt.Errorf("copy %s to %s: path already exists: %w", request.SrcPath, request.DstPath, errord.ErrAlreadyExists)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return classifyPathError("copy", request.DstPath, err)
	}
	if src.IsDir() {
		if !request.Recursive {
			return fmt.Errorf("copy %s: source is a directory: %w", request.SrcPath, errord.ErrFailedPrecondition)
		}
		return copyDir(request.SrcPath, request.DstPath, request.Overwrite)
	}
	return copyFile(request.SrcPath, request.DstPath, src.Mode().Perm(), request.Overwrite)
}

func (s *Service) Move(request MoveRequest) error {
	if strings.TrimSpace(request.SrcPath) == "" || strings.TrimSpace(request.DstPath) == "" {
		return fmt.Errorf("src_path and dst_path are required: %w", errord.ErrInvalidArgument)
	}
	if _, err := os.Lstat(request.SrcPath); err != nil {
		return classifyPathError("move", request.SrcPath, err)
	}
	if _, err := os.Lstat(request.DstPath); err == nil {
		if !request.Overwrite {
			return fmt.Errorf("move %s to %s: path already exists: %w", request.SrcPath, request.DstPath, errord.ErrAlreadyExists)
		}
		if err := os.RemoveAll(request.DstPath); err != nil {
			return classifyPathError("move", request.DstPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return classifyPathError("move", request.DstPath, err)
	}
	if err := os.Rename(request.SrcPath, request.DstPath); err != nil {
		return classifyPathError("move", request.SrcPath, err)
	}
	return nil
}

func (s *Service) Chmod(request ChmodRequest) error {
	if strings.TrimSpace(request.Path) == "" {
		return fmt.Errorf("path is required: %w", errord.ErrInvalidArgument)
	}
	mode := os.FileMode(request.Mode).Perm()
	if request.Recursive {
		return filepath.WalkDir(request.Path, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return classifyPathError("chmod", path, err)
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if err := os.Chmod(path, mode); err != nil {
				return classifyPathError("chmod", path, err)
			}
			return nil
		})
	}
	if err := os.Chmod(request.Path, mode); err != nil {
		return classifyPathError("chmod", request.Path, err)
	}
	return nil
}

func (s *Service) Touch(request TouchRequest) error {
	if strings.TrimSpace(request.Path) == "" {
		return fmt.Errorf("path is required: %w", errord.ErrInvalidArgument)
	}
	if _, err := os.Lstat(request.Path); err != nil {
		if !request.Create || !errors.Is(err, os.ErrNotExist) {
			return classifyPathError("touch", request.Path, err)
		}
		file, err := os.OpenFile(request.Path, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return classifyPathError("touch", request.Path, err)
		}
		if err := file.Close(); err != nil {
			return classifyPathError("touch", request.Path, err)
		}
	}
	mtime := time.Now()
	if request.MtimeNs != 0 {
		mtime = time.Unix(0, request.MtimeNs)
	}
	if err := os.Chtimes(request.Path, mtime, mtime); err != nil {
		return classifyPathError("touch", request.Path, err)
	}
	return nil
}

func fileInfo(path string) (*filev1.SandboxFileInfo, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("path is required: %w", errord.ErrInvalidArgument)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, classifyPathError("stat", path, err)
	}
	return &filev1.SandboxFileInfo{
		Path:    path,
		Kind:    fileKind(info),
		Size:    info.Size(),
		Mode:    uint32(info.Mode().Perm()),
		MtimeNs: info.ModTime().UnixNano(),
	}, nil
}

func fileKind(info os.FileInfo) filev1.SandboxFileKind {
	mode := info.Mode()
	switch {
	case mode.IsDir():
		return filev1.SandboxFileKind_SANDBOX_FILE_KIND_DIRECTORY
	case mode&os.ModeSymlink != 0:
		return filev1.SandboxFileKind_SANDBOX_FILE_KIND_SYMLINK
	case mode.IsRegular():
		return filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE
	default:
		return filev1.SandboxFileKind_SANDBOX_FILE_KIND_OTHER
	}
}

func classifyPathError(operation string, path string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%s %s: %w", operation, path, errord.ErrNotFound)
	case errors.Is(err, os.ErrExist):
		return fmt.Errorf("%s %s: %w", operation, path, errord.ErrAlreadyExists)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%s %s: %w", operation, path, errord.ErrFailedPrecondition)
	default:
		return fmt.Errorf("%s %s: %v: %w", operation, path, err, errord.ErrFailedPrecondition)
	}
}

func StatusCode(err error) int {
	switch {
	case errors.Is(err, ErrMethodNotAllowed):
		return 405
	case errord.IsInvalidArgument(err):
		return 400
	case errord.IsNotFound(err):
		return 404
	case errord.IsAlreadyExists(err):
		return 409
	case errord.IsFailedPrecondition(err):
		return 412
	default:
		return 500
	}
}
