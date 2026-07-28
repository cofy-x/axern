package server

import (
	"bytes"
	"io"
	"net/http"
	"strconv"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/fileapi"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
)

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.files == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "file service unavailable")
		return
	}
	path := r.URL.Query().Get("path")
	var (
		response any
		err      error
	)
	switch r.URL.Path {
	case wire.PathFileStat:
		response, err = s.files.Stat(path)
	case wire.PathFileList:
		response, err = s.files.List(path)
	case wire.PathFileRead:
		response, err = s.files.Read(path)
	case wire.PathFileExists:
		response, err = s.files.Exists(path)
	case wire.PathFileWrite:
		err = decodeFileRequest(r, &fileapi.WriteRequest{}, func(request fileapi.WriteRequest) error { return s.files.Write(request) })
		response = okResponse()
	case wire.PathFileMkdir:
		err = decodeFileRequest(r, &fileapi.MkdirRequest{}, func(request fileapi.MkdirRequest) error { return s.files.Mkdir(request) })
		response = okResponse()
	case wire.PathFileRemove:
		err = decodeFileRequest(r, &fileapi.RemoveRequest{}, func(request fileapi.RemoveRequest) error { return s.files.Remove(request) })
		response = okResponse()
	case wire.PathFileCopy:
		err = decodeFileRequest(r, &fileapi.CopyRequest{}, func(request fileapi.CopyRequest) error { return s.files.Copy(request) })
		response = okResponse()
	case wire.PathFileMove:
		err = decodeFileRequest(r, &fileapi.MoveRequest{}, func(request fileapi.MoveRequest) error { return s.files.Move(request) })
		response = okResponse()
	case wire.PathFileChmod:
		err = decodeFileRequest(r, &fileapi.ChmodRequest{}, func(request fileapi.ChmodRequest) error { return s.files.Chmod(request) })
		response = okResponse()
	case wire.PathFileTouch:
		err = decodeFileRequest(r, &fileapi.TouchRequest{}, func(request fileapi.TouchRequest) error { return s.files.Touch(request) })
		response = okResponse()
	case wire.PathArchiveUpload:
		if r.Method != http.MethodPost {
			err = fileapi.ErrMethodNotAllowed
			break
		}
		err = s.files.UploadArchive(fileapi.UploadArchiveOptions{
			Path:          path,
			Format:        archiveFormat(r),
			CreateParents: queryBool(r, "createParents"),
			Overwrite:     queryBool(r, "overwrite"),
			SymlinkPolicy: archiveSymlinkPolicy(r),
		}, r.Body)
		response = okResponse()
	case wire.PathArchiveDownload:
		if r.Method != http.MethodGet {
			err = fileapi.ErrMethodNotAllowed
			break
		}
		err = s.handleFileArchiveDownload(w, r, path)
	default:
		writeNotFound(w)
		return
	}
	if err != nil {
		writeError(w, fileapi.StatusCode(err), "", err.Error())
		return
	}
	if response != nil {
		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleFileArchiveDownload(w http.ResponseWriter, r *http.Request, path string) error {
	var archive bytes.Buffer
	if err := s.files.DownloadArchive(fileapi.DownloadArchiveOptions{
		Path:          path,
		Format:        archiveFormat(r),
		SymlinkPolicy: archiveSymlinkPolicy(r),
	}, &archive); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, &archive)
	return err
}

func decodeFileRequest[T any](r *http.Request, target *T, apply func(T) error) error {
	if r.Method != http.MethodPost {
		return fileapi.ErrMethodNotAllowed
	}
	if err := decodeRequiredJSONRequest(r, target); err != nil {
		return fileapi.ErrInvalidJSON
	}
	return apply(*target)
}

func okResponse() map[string]bool {
	return map[string]bool{"ok": true}
}

func queryBool(r *http.Request, key string) bool {
	value, _ := strconv.ParseBool(r.URL.Query().Get(key))
	return value
}

func archiveFormat(r *http.Request) filev1.SandboxArchiveFormat {
	value, _ := strconv.Atoi(r.URL.Query().Get("format"))
	return filev1.SandboxArchiveFormat(value)
}

func archiveSymlinkPolicy(r *http.Request) filev1.SandboxArchiveSymlinkPolicy {
	value, _ := strconv.Atoi(r.URL.Query().Get("symlinkPolicy"))
	return filev1.SandboxArchiveSymlinkPolicy(value)
}
