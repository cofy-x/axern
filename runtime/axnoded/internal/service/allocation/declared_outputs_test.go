package allocation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

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

type stoppedWorkloadFileService struct {
	declaredOutputFileService
	stopped func() bool
}

func (f stoppedWorkloadFileService) StatFile(ctx context.Context, request *apipb.StatFileRequest, options contract.HandlerOptions) (*apipb.StatFileResponse, error) {
	if !f.stopped() {
		return nil, errord.ErrFailedPrecondition
	}
	return f.declaredOutputFileService.StatFile(ctx, request, options)
}

func TestCleanupDeclaredOutputsUsesAuthoritativeContractAndRejectsConflict(t *testing.T) {
	fixture := newTestAllocationController(t, &runtimeSpyHandler{name: "runsc"})
	local := []*commonv1.DeclaredOutput{{Path: "/tmp/output.patch", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE, MediaType: "text/x-diff"}}
	if err := fixture.controller.StoreAllocationIntent(
		"allocation-output-contract", "node-a",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		time.Now().Add(time.Minute), nil, nil, local, nil,
	); err != nil {
		t.Fatal(err)
	}

	requested := []*commonv1.DeclaredOutput{{Path: "/tmp/output.patch", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE, MediaType: "text/x-diff"}}
	resolved, present, err := fixture.controller.cleanupDeclaredOutputs("allocation-output-contract", requested)
	if err != nil || !present || len(resolved) != 1 || resolved[0].GetPath() != "/tmp/output.patch" {
		t.Fatalf("cleanupDeclaredOutputs() = %#v, %v", resolved, err)
	}
	requested[0].Path = "/tmp/mutated"
	if resolved[0].GetPath() != "/tmp/output.patch" {
		t.Fatal("cleanup contract aliases the caller request")
	}

	_, _, err = fixture.controller.cleanupDeclaredOutputs("allocation-output-contract", []*commonv1.DeclaredOutput{{
		Path: "/tmp/different", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE,
	}})
	if err == nil {
		t.Fatal("cleanupDeclaredOutputs() accepted a conflicting durable contract")
	}

	recovered, present, err := fixture.controller.cleanupDeclaredOutputs("allocation-state-missing", local)
	if err != nil || present || len(recovered) != 1 || recovered[0].GetPath() != "/tmp/output.patch" {
		t.Fatalf("cleanupDeclaredOutputs() without local state = %#v, %v", recovered, err)
	}
	if err := fixture.controller.StoreAllocationIntent(
		"allocation-zero-outputs", "node-a",
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		time.Now().Add(time.Minute), nil, nil, nil, nil,
	); err != nil {
		t.Fatal(err)
	}
	zero, present, err := fixture.controller.cleanupDeclaredOutputs("allocation-zero-outputs", nil)
	if err != nil || !present || len(zero) != 0 {
		t.Fatalf("explicit zero-output contract = %#v, present=%t, err=%v", zero, present, err)
	}
}

func TestDeclaredOutputContractDigestIsDeterministicAndOrderSensitive(t *testing.T) {
	empty, err := declaredOutputContractSHA256(nil)
	if err != nil || empty != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty contract digest = %q, %v", empty, err)
	}
	left := []*commonv1.DeclaredOutput{
		{Path: "/tmp/one", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE},
		{Path: "/tmp/two", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR},
	}
	first, err := declaredOutputContractSHA256(left)
	if err != nil {
		t.Fatal(err)
	}
	second, err := declaredOutputContractSHA256(cloneDeclaredOutputs(left))
	if err != nil || first != second {
		t.Fatalf("deterministic digest = %q / %q, %v", first, second, err)
	}
	reversed, err := declaredOutputContractSHA256([]*commonv1.DeclaredOutput{left[1], left[0]})
	if err != nil || reversed == first {
		t.Fatalf("reordered contract digest = %q, want different from %q", reversed, first)
	}
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
	content := []byte("declared output")
	entry, err := captureDeclaredOutput(
		context.Background(),
		declaredOutputFileService{kind: filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE, content: content},
		contract.HandlerOptions{ContainerID: "allocation-one"},
		&commonv1.DeclaredOutput{Path: "/workspace/output.patch", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE, MediaType: "text/x-diff"},
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
