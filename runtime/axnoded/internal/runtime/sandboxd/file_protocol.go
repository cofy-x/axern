package sandboxd

import filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"

type FileStatResponse struct {
	Info *filev1.SandboxFileInfo `json:"info"`
}

type FileListResponse struct {
	Entries []*filev1.SandboxFileInfo `json:"entries"`
}

type FileReadResponse struct {
	Data []byte `json:"data"`
}

type FileExistsResponse struct {
	Exists bool `json:"exists"`
}

type FileWriteRequest struct {
	Path          string `json:"path"`
	Data          []byte `json:"data"`
	CreateParents bool   `json:"createParents"`
}

type FileMkdirRequest struct {
	Path    string `json:"path"`
	Parents bool   `json:"parents"`
}

type FileRemoveRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	Force     bool   `json:"force"`
}

type FileCopyRequest struct {
	SrcPath   string `json:"srcPath"`
	DstPath   string `json:"dstPath"`
	Recursive bool   `json:"recursive"`
	Overwrite bool   `json:"overwrite"`
}

type FileMoveRequest struct {
	SrcPath   string `json:"srcPath"`
	DstPath   string `json:"dstPath"`
	Overwrite bool   `json:"overwrite"`
}

type FileChmodRequest struct {
	Path      string `json:"path"`
	Mode      uint32 `json:"mode"`
	Recursive bool   `json:"recursive"`
}

type FileTouchRequest struct {
	Path    string `json:"path"`
	Create  bool   `json:"create"`
	MtimeNs int64  `json:"mtimeNs"`
}

type FileArchiveUploadRequest struct {
	Path          string
	Format        filev1.SandboxArchiveFormat
	CreateParents bool
	Overwrite     bool
	SymlinkPolicy filev1.SandboxArchiveSymlinkPolicy
}

type FileArchiveDownloadRequest struct {
	Path          string
	Format        filev1.SandboxArchiveFormat
	SymlinkPolicy filev1.SandboxArchiveSymlinkPolicy
}
