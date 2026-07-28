package managedrollout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	artifactv1 "github.com/cofy-x/axern/sdk/go/gen/axern/data/artifact/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxTicketRefreshes = 16

type DownloadParams struct {
	ArtifactID  string
	Destination string
	Force       bool
}

type Downloader struct {
	control rolloutv1.RolloutControlClient
	data    artifactv1.ArtifactDataClient
}

func NewDownloader(control rolloutv1.RolloutControlClient, data artifactv1.ArtifactDataClient) Downloader {
	return Downloader{control: control, data: data}
}

func (d Downloader) Download(ctx context.Context, params DownloadParams) (*rolloutv1.Artifact, error) {
	if d.control == nil || d.data == nil {
		return nil, fmt.Errorf("artifact download clients are required")
	}
	destination := filepath.Clean(params.Destination)
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode().IsRegular() && !params.Force {
			return nil, fmt.Errorf("destination already exists: %s", destination)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("destination is not a regular file: %s", destination)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return nil, err
	}
	part := destination + ".part"
	var artifact *rolloutv1.Artifact
	refreshes := 0
	for {
		offset, err := partialOffset(part)
		if err != nil {
			return nil, err
		}
		prepared, err := d.control.PrepareArtifactDownload(ctx, &rolloutv1.PrepareArtifactDownloadRequest{ArtifactID: params.ArtifactID})
		if err != nil {
			return nil, err
		}
		artifact = prepared.GetArtifact()
		if artifact == nil {
			return nil, fmt.Errorf("artifact metadata is empty")
		}
		if offset > artifact.GetSizeBytes() {
			return nil, fmt.Errorf("partial file exceeds artifact size")
		}
		if offset == artifact.GetSizeBytes() {
			// A valid zero-byte artifact has no stream to create its partial
			// file. Materialize it here so the common integrity and atomic
			// replacement path also handles empty evidence.
			file, err := openPartial(part)
			if err != nil {
				return nil, err
			}
			if err := file.Close(); err != nil {
				return nil, err
			}
			break
		}
		stream, err := d.data.DownloadArtifact(ctx, &artifactv1.DownloadArtifactRequest{
			Ticket: prepared.GetTicket(),
			Offset: offset,
		})
		if err != nil {
			if refreshableTicketError(err) && refreshes < maxTicketRefreshes {
				refreshes++
				continue
			}
			return nil, err
		}
		file, err := openPartial(part)
		if err != nil {
			return nil, err
		}
		expected := offset
		retry := false
		for {
			chunk, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				break
			}
			if recvErr != nil {
				_ = file.Close()
				if refreshableTicketError(recvErr) && refreshes < maxTicketRefreshes {
					refreshes++
					retry = true
					break
				}
				return nil, recvErr
			}
			if chunk.GetOffset() != expected {
				_ = file.Close()
				return nil, fmt.Errorf("artifact stream offset mismatch")
			}
			written, writeErr := file.Write(chunk.GetData())
			if writeErr != nil {
				_ = file.Close()
				return nil, writeErr
			}
			if written != len(chunk.GetData()) {
				_ = file.Close()
				return nil, io.ErrShortWrite
			}
			expected += int64(written)
		}
		if retry {
			continue
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
		break
	}
	if err := verifyPart(part, artifact); err != nil {
		return nil, err
	}
	if err := replaceFile(part, destination, params.Force); err != nil {
		return nil, err
	}
	return artifact, nil
}

func SafeName(id, name string) string {
	cleanComponent := func(value string) string {
		clean := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, filepath.Base(value))
		return strings.Trim(clean, ".")
	}
	cleanID := cleanComponent(id)
	clean := cleanComponent(name)
	if cleanID == "" {
		cleanID = "artifact"
	}
	if clean == "" {
		clean = "artifact"
	}
	return cleanID + "-" + clean
}

func PrepareOutputDirectory(path string) (string, error) {
	path = filepath.Clean(path)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("output directory must be a real directory: %s", path)
	}
	return path, nil
}

func partialOffset(part string) (int64, error) {
	info, err := os.Lstat(part)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("partial destination is not a regular file: %s", part)
	}
	return info.Size(), nil
}

func openPartial(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	closeWithError := func(err error) (*os.File, error) {
		_ = file.Close()
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return closeWithError(err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return closeWithError(err)
	}
	if !opened.Mode().IsRegular() || !pathInfo.Mode().IsRegular() || !os.SameFile(opened, pathInfo) {
		return closeWithError(fmt.Errorf("partial destination must remain a regular file: %s", path))
	}
	if err := file.Chmod(0o600); err != nil {
		return closeWithError(err)
	}
	return file, nil
}

func verifyPart(part string, artifact *rolloutv1.Artifact) error {
	info, err := os.Stat(part)
	if err != nil {
		return err
	}
	if info.Size() != artifact.GetSizeBytes() {
		return fmt.Errorf("artifact size mismatch: got %d want %d", info.Size(), artifact.GetSizeBytes())
	}
	file, err := os.Open(part)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	got := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, artifact.GetDigest()) {
		return fmt.Errorf("artifact digest mismatch")
	}
	return nil
}

func replaceFile(part, destination string, force bool) error {
	if force {
		return os.Rename(part, destination)
	}
	// The hard-link creation is the portable same-filesystem no-replace
	// primitive. The partial file is adjacent to destination, so linking is
	// atomic and cannot overwrite a file created by a concurrent process.
	if err := os.Link(part, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("destination already exists: %s", destination)
		}
		return err
	}
	return os.Remove(part)
}

func refreshableTicketError(err error) bool {
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated:
		return true
	default:
		return false
	}
}
