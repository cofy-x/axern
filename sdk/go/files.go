package axernsdk

import (
	"context"

	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
)

// SandboxFileKind describes the kind of a sandbox filesystem entry.
type SandboxFileKind string

const (
	SandboxFileKindFile        SandboxFileKind = "file"
	SandboxFileKindDirectory   SandboxFileKind = "directory"
	SandboxFileKindSymlink     SandboxFileKind = "symlink"
	SandboxFileKindOther       SandboxFileKind = "other"
	SandboxFileKindUnspecified SandboxFileKind = "unspecified"
)

// SandboxFileInfo contains sandbox filesystem metadata.
type SandboxFileInfo struct {
	Path    string
	Kind    SandboxFileKind
	Size    int64
	Mode    uint32
	MtimeNS int64
}

// WriteFileOptions configures sandbox file writes.
type WriteFileOptions struct {
	CreateParents bool
}

// MkdirOptions configures sandbox directory creation.
type MkdirOptions struct {
	Parents bool
}

// RemoveOptions configures sandbox file or directory removal.
type RemoveOptions struct {
	Recursive bool
	Force     bool
}

// CopyOptions configures sandbox-side copy operations.
type CopyOptions struct {
	Recursive bool
	Overwrite bool
}

// MoveOptions configures sandbox-side move operations.
type MoveOptions struct {
	Overwrite bool
}

// ChmodOptions configures sandbox-side chmod operations.
type ChmodOptions struct {
	Recursive bool
}

// TouchOptions configures sandbox-side touch operations.
type TouchOptions struct {
	NoCreate bool
	MtimeNS  int64
}

// Stat returns metadata for a sandbox path.
func (s *Sandbox) Stat(ctx context.Context, path string) (SandboxFileInfo, error) {
	node, err := s.nodeClient()
	if err != nil {
		return SandboxFileInfo{}, err
	}
	return node.Stat(ctx, path)
}

// ListDir returns direct entries for a sandbox directory.
func (s *Sandbox) ListDir(ctx context.Context, path string) ([]SandboxFileInfo, error) {
	node, err := s.nodeClient()
	if err != nil {
		return nil, err
	}
	return node.ListDir(ctx, path)
}

// Exists reports whether a sandbox path exists.
func (s *Sandbox) Exists(ctx context.Context, path string) (bool, error) {
	node, err := s.nodeClient()
	if err != nil {
		return false, err
	}
	return node.Exists(ctx, path)
}

// ReadFile reads a sandbox file as bytes.
func (s *Sandbox) ReadFile(ctx context.Context, path string) ([]byte, error) {
	node, err := s.nodeClient()
	if err != nil {
		return nil, err
	}
	return node.ReadFile(ctx, path)
}

// WriteFile writes bytes to a sandbox file.
func (s *Sandbox) WriteFile(ctx context.Context, path string, data []byte, options WriteFileOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.WriteFile(ctx, path, data, options)
}

// Mkdir creates a sandbox directory.
func (s *Sandbox) Mkdir(ctx context.Context, path string, options MkdirOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.Mkdir(ctx, path, options)
}

// Remove removes a sandbox file or directory.
func (s *Sandbox) Remove(ctx context.Context, path string, options RemoveOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.Remove(ctx, path, options)
}

// Copy copies a sandbox file or directory.
func (s *Sandbox) Copy(ctx context.Context, srcPath, dstPath string, options CopyOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.Copy(ctx, srcPath, dstPath, options)
}

// Move moves a sandbox file or directory.
func (s *Sandbox) Move(ctx context.Context, srcPath, dstPath string, options MoveOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.Move(ctx, srcPath, dstPath, options)
}

// Chmod changes sandbox file permissions.
func (s *Sandbox) Chmod(ctx context.Context, path string, mode uint32, options ChmodOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.Chmod(ctx, path, mode, options)
}

// Touch updates sandbox file timestamps, optionally creating the path.
func (s *Sandbox) Touch(ctx context.Context, path string, options TouchOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.Touch(ctx, path, options)
}

func sandboxFileInfo(info *filev1.SandboxFileInfo) SandboxFileInfo {
	if info == nil {
		return SandboxFileInfo{Kind: SandboxFileKindUnspecified}
	}
	return SandboxFileInfo{
		Path:    info.GetPath(),
		Kind:    sandboxFileKind(info.GetKind()),
		Size:    info.GetSize(),
		Mode:    info.GetMode(),
		MtimeNS: info.GetMtimeNs(),
	}
}

func sandboxFileInfos(entries []*filev1.SandboxFileInfo) []SandboxFileInfo {
	if len(entries) == 0 {
		return nil
	}
	infos := make([]SandboxFileInfo, 0, len(entries))
	for _, entry := range entries {
		infos = append(infos, sandboxFileInfo(entry))
	}
	return infos
}

func sandboxFileKind(kind filev1.SandboxFileKind) SandboxFileKind {
	switch kind {
	case filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE:
		return SandboxFileKindFile
	case filev1.SandboxFileKind_SANDBOX_FILE_KIND_DIRECTORY:
		return SandboxFileKindDirectory
	case filev1.SandboxFileKind_SANDBOX_FILE_KIND_SYMLINK:
		return SandboxFileKindSymlink
	case filev1.SandboxFileKind_SANDBOX_FILE_KIND_OTHER:
		return SandboxFileKindOther
	default:
		return SandboxFileKindUnspecified
	}
}
