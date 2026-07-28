package managedrollout

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	artifactv1 "github.com/cofy-x/axern/sdk/go/gen/axern/data/artifact/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeDownloadControl struct {
	rolloutv1.RolloutControlClient
	artifact *rolloutv1.Artifact
	calls    int
}

func (f *fakeDownloadControl) PrepareArtifactDownload(context.Context, *rolloutv1.PrepareArtifactDownloadRequest, ...grpc.CallOption) (*rolloutv1.PrepareArtifactDownloadResponse, error) {
	f.calls++
	return &rolloutv1.PrepareArtifactDownloadResponse{Artifact: f.artifact, Ticket: fmt.Sprintf("ticket-%d", f.calls)}, nil
}

type fakeData struct {
	artifactv1.ArtifactDataClient
	calls int
}

func (f *fakeData) DownloadArtifact(_ context.Context, request *artifactv1.DownloadArtifactRequest, _ ...grpc.CallOption) (artifactv1.ArtifactData_DownloadArtifactClient, error) {
	f.calls++
	if f.calls == 1 {
		return nil, status.Error(codes.PermissionDenied, "expired")
	}
	return &fakeArtifactStream{chunks: []*artifactv1.DownloadArtifactResponse{{Offset: request.GetOffset(), Data: []byte("cd")}}}, nil
}

type fakeArtifactStream struct {
	grpc.ClientStream
	chunks []*artifactv1.DownloadArtifactResponse
}

func (s *fakeArtifactStream) Recv() (*artifactv1.DownloadArtifactResponse, error) {
	if len(s.chunks) == 0 {
		return nil, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}

func TestDownloadResumesAndRefreshesExpiredTicket(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "evidence.json")
	if err := os.WriteFile(destination+".part", []byte("ab"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("abcd"))
	control := &fakeDownloadControl{artifact: &rolloutv1.Artifact{ID: "art-test", SizeBytes: 4, Digest: fmt.Sprintf("sha256:%x", sum)}}
	data := &fakeData{}
	downloader := NewDownloader(control, data)
	if _, err := downloader.Download(context.Background(), DownloadParams{
		ArtifactID:  "art-test",
		Destination: destination,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abcd" || control.calls != 2 || data.calls != 2 {
		t.Fatalf("content=%q prepare=%d download=%d", got, control.calls, data.calls)
	}
}

func TestSafeNameCannotEscapeDirectory(t *testing.T) {
	if got := SafeName("art-1", "../../etc/passwd"); got != "art-1-passwd" {
		t.Fatalf("SafeName() = %q", got)
	}
	if got := SafeName("../../escaped", "evidence.json"); got != "escaped-evidence.json" {
		t.Fatalf("SafeName() with hostile ID = %q", got)
	}
}

func TestPrepareOutputDirectoryRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareOutputDirectory(link); err == nil {
		t.Fatal("symlink output directory was accepted")
	}
}

func TestDownloadRejectsSymlinkPartialFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "evidence.json")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, destination+".part"); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("x"))
	control := &fakeDownloadControl{artifact: &rolloutv1.Artifact{ID: "art-test", SizeBytes: 1, Digest: fmt.Sprintf("sha256:%x", sum)}}
	downloader := NewDownloader(control, &fakeData{})
	if _, err := downloader.Download(context.Background(), DownloadParams{
		ArtifactID:  "art-test",
		Destination: destination,
	}); err == nil {
		t.Fatal("symlink partial file was accepted")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("symlink target size = %d, want 0", info.Size())
	}
}

func TestDownloadFinishesAlreadyCompletePartialFileWithoutRangeRequest(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "evidence.json")
	content := []byte("complete")
	if err := os.WriteFile(destination+".part", content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	control := &fakeDownloadControl{artifact: &rolloutv1.Artifact{ID: "art-test", SizeBytes: int64(len(content)), Digest: fmt.Sprintf("sha256:%x", sum)}}
	data := &fakeData{}
	downloader := NewDownloader(control, data)
	if _, err := downloader.Download(context.Background(), DownloadParams{
		ArtifactID:  "art-test",
		Destination: destination,
	}); err != nil {
		t.Fatal(err)
	}
	if data.calls != 0 {
		t.Fatalf("DownloadArtifact calls = %d, want 0", data.calls)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q", got)
	}
}

func TestDownloadMaterializesEmptyArtifactWithoutRangeRequest(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "empty.txt")
	sum := sha256.Sum256(nil)
	control := &fakeDownloadControl{artifact: &rolloutv1.Artifact{ID: "art-empty", Digest: fmt.Sprintf("sha256:%x", sum)}}
	data := &fakeData{}
	downloader := NewDownloader(control, data)
	if _, err := downloader.Download(context.Background(), DownloadParams{
		ArtifactID:  "art-empty",
		Destination: destination,
	}); err != nil {
		t.Fatal(err)
	}
	if data.calls != 0 {
		t.Fatalf("DownloadArtifact calls = %d, want 0", data.calls)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("size = %d, want 0", info.Size())
	}
}
