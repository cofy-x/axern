package allocation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type declaredOutputFileService struct {
	contract.FileService
	kind    filev1.SandboxFileKind
	content []byte
	statErr error
	readErr error
}

func (f declaredOutputFileService) StatFile(context.Context, *apipb.StatFileRequest, contract.HandlerOptions) (*apipb.StatFileResponse, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return &apipb.StatFileResponse{Info: &filev1.SandboxFileInfo{Kind: f.kind, Size: int64(len(f.content))}}, nil
}

func (f declaredOutputFileService) ReadFile(context.Context, *apipb.ReadFileRequest, contract.HandlerOptions) (*apipb.ReadFileResponse, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return &apipb.ReadFileResponse{Data: append([]byte(nil), f.content...)}, nil
}

func (f declaredOutputFileService) DownloadArchive(_ context.Context, _ *apipb.DownloadArchiveRequest, output io.Writer, _ contract.HandlerOptions) (*apipb.DownloadArchiveResponse, error) {
	_, err := output.Write(f.content)
	return &apipb.DownloadArchiveResponse{}, err
}

func TestCaptureDeclaredFileProducesImmutableObjectMetadata(t *testing.T) {
	objects := t.TempDir()
	content := []byte("candidate bundle")
	entry, err := captureDeclaredOutput(
		context.Background(),
		declaredOutputFileService{kind: filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE, content: content},
		contract.HandlerOptions{ContainerID: "allocation-one"},
		&commonv1.DeclaredOutput{Path: "/workspace/candidate.patch", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE, MediaType: "text/x-diff"},
		objects,
		maxSealedTotalBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(content)
	if entry.Status != "available" || entry.SizeBytes != int64(len(content)) || entry.SHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("entry = %+v", entry)
	}
	got, err := os.ReadFile(filepath.Join(objects, entry.Object))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("object = %q", got)
	}
}

func TestCaptureDeclaredOutputClassifiesMissingAndUnsafeKinds(t *testing.T) {
	declaration := &commonv1.DeclaredOutput{Path: "/workspace/output", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE}
	missing, err := captureDeclaredOutput(context.Background(), declaredOutputFileService{statErr: errord.ErrNotFound}, contract.HandlerOptions{ContainerID: "allocation-one"}, declaration, t.TempDir(), maxSealedTotalBytes)
	if err != nil || missing.Status != "missing" {
		t.Fatalf("missing = %+v, %v", missing, err)
	}
	symlink, err := captureDeclaredOutput(context.Background(), declaredOutputFileService{kind: filev1.SandboxFileKind_SANDBOX_FILE_KIND_SYMLINK}, contract.HandlerOptions{ContainerID: "allocation-one"}, declaration, t.TempDir(), maxSealedTotalBytes)
	if err != nil || symlink.Status != "rejected" {
		t.Fatalf("symlink = %+v, %v", symlink, err)
	}
	disappeared, err := captureDeclaredOutput(context.Background(), declaredOutputFileService{kind: filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE, content: []byte("x"), readErr: errord.ErrNotFound}, contract.HandlerOptions{ContainerID: "allocation-one"}, declaration, t.TempDir(), maxSealedTotalBytes)
	if err != nil || disappeared.Status != "capture_failed" || disappeared.Reason == "" {
		t.Fatalf("disappeared = %+v, %v", disappeared, err)
	}
}

func TestCaptureDeclaredArchiveRejectsOversizeWithoutPublishingObject(t *testing.T) {
	objects := t.TempDir()
	entry, err := captureDeclaredOutput(
		context.Background(),
		declaredOutputFileService{kind: filev1.SandboxFileKind_SANDBOX_FILE_KIND_DIRECTORY, content: []byte("archive")},
		contract.HandlerOptions{ContainerID: "allocation-one"},
		&commonv1.DeclaredOutput{Path: "/workspace/result", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR},
		objects,
		1,
	)
	if err != nil || entry.Status != "rejected" {
		t.Fatalf("entry = %+v, %v", entry, err)
	}
	entries, err := os.ReadDir(objects)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected output published %d objects", len(entries))
	}
}
