package fileapi

import (
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
)

var (
	ErrInvalidJSON      = fmt.Errorf("invalid file request: %w", errord.ErrInvalidArgument)
	ErrMethodNotAllowed = fmt.Errorf("method not allowed: %w", errord.ErrFailedPrecondition)
)

type StatResponse struct {
	Info *filev1.SandboxFileInfo `json:"info"`
}

type ListResponse struct {
	Entries []*filev1.SandboxFileInfo `json:"entries"`
}

type ReadResponse struct {
	Data []byte `json:"data"`
}

type ExistsResponse struct {
	Exists bool `json:"exists"`
}

type WriteRequest struct {
	Path          string `json:"path"`
	Data          []byte `json:"data"`
	CreateParents bool   `json:"createParents"`
}

type MkdirRequest struct {
	Path    string `json:"path"`
	Parents bool   `json:"parents"`
}

type RemoveRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	Force     bool   `json:"force"`
}

type CopyRequest struct {
	SrcPath   string `json:"srcPath"`
	DstPath   string `json:"dstPath"`
	Recursive bool   `json:"recursive"`
	Overwrite bool   `json:"overwrite"`
}

type MoveRequest struct {
	SrcPath   string `json:"srcPath"`
	DstPath   string `json:"dstPath"`
	Overwrite bool   `json:"overwrite"`
}

type ChmodRequest struct {
	Path      string `json:"path"`
	Mode      uint32 `json:"mode"`
	Recursive bool   `json:"recursive"`
}

type TouchRequest struct {
	Path    string `json:"path"`
	Create  bool   `json:"create"`
	MtimeNs int64  `json:"mtimeNs"`
}

type UploadArchiveOptions struct {
	Path          string
	Format        filev1.SandboxArchiveFormat
	CreateParents bool
	Overwrite     bool
	SymlinkPolicy filev1.SandboxArchiveSymlinkPolicy
}

type DownloadArchiveOptions struct {
	Path          string
	Format        filev1.SandboxArchiveFormat
	SymlinkPolicy filev1.SandboxArchiveSymlinkPolicy
}
