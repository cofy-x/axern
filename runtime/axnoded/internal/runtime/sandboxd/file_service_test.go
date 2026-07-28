package sandboxd

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileServiceUsesSandboxdForReadOnlyOperations(t *testing.T) {
	service, socketPath, expectedSocket := newTestFileService(t, &fakeFileClient{
		stat:   FileStatResponse{Info: &filev1.SandboxFileInfo{Path: "/tmp/message.txt", Size: 5}},
		list:   FileListResponse{Entries: []*filev1.SandboxFileInfo{{Path: "/tmp/message.txt"}}},
		read:   FileReadResponse{Data: []byte("hello")},
		exists: FileExistsResponse{Exists: true},
	})

	options := fileServiceTestOptions()

	stat, err := service.StatFile(context.Background(), &apipb.StatFileRequest{Path: "/tmp/message.txt"}, options)
	require.NoError(t, err)
	assert.Equal(t, int64(5), stat.GetInfo().GetSize())
	assert.Equal(t, expectedSocket, *socketPath)

	list, err := service.ListDir(context.Background(), &apipb.ListDirRequest{Path: "/tmp"}, options)
	require.NoError(t, err)
	assert.Len(t, list.GetEntries(), 1)

	read, err := service.ReadFile(context.Background(), &apipb.ReadFileRequest{Path: "/tmp/message.txt"}, options)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), read.GetData())

	exists, err := service.Exists(context.Background(), &apipb.ExistsRequest{Path: "/tmp/message.txt"}, options)
	require.NoError(t, err)
	assert.True(t, exists.GetExists())
}

func TestFileServiceRequiresContainerID(t *testing.T) {
	service := NewFileService(t.TempDir())

	_, err := service.ReadFile(context.Background(), &apipb.ReadFileRequest{Path: "/tmp/message.txt"}, contract.HandlerOptions{})

	require.Error(t, err)
	assert.True(t, errord.IsInvalidArgument(err))
	assert.Contains(t, err.Error(), "requires container id")
}

func TestFileServiceMapsSandboxdStatusErrors(t *testing.T) {
	service, _, _ := newTestFileService(t, &fakeFileClient{statErr: &StatusError{Path: "/files/stat", StatusCode: 404, Message: "missing"}})

	_, err := service.StatFile(context.Background(), &apipb.StatFileRequest{Path: "/missing"}, fileServiceTestOptions())

	assert.True(t, errord.IsNotFound(err))
}

func TestArchiveOperationsRequireArchiveCapability(t *testing.T) {
	service, _, _ := newTestFileService(t, &fakeFileClient{})
	options := fileServiceTestOptions()
	options.ContainerLabels = map[string]string{
		LabelReady:        "true",
		LabelSocket:       "/tmp/sandboxd.sock",
		LabelCapabilities: "file,process",
	}

	_, err := service.UploadArchive(context.Background(), &apipb.UploadArchiveRequest{Path: "/tmp/tree"}, bytes.NewReader(nil), options)

	assert.True(t, errord.IsFailedPrecondition(err))
	assert.Contains(t, err.Error(), "archive capability unavailable")
}

func TestArchiveOperationsDoNotRequireFileCapability(t *testing.T) {
	client := &fakeFileClient{}
	service, _, _ := newTestFileService(t, client)
	options := fileServiceTestOptions()
	options.ContainerLabels = map[string]string{
		LabelReady:        "true",
		LabelSocket:       "/tmp/sandboxd.sock",
		LabelCapabilities: "archive",
	}

	_, err := service.UploadArchive(context.Background(), &apipb.UploadArchiveRequest{Path: "/tmp/tree"}, bytes.NewReader([]byte("archive")), options)

	require.NoError(t, err)
	assert.Equal(t, []string{"upload"}, client.mutations)
}

func newTestFileService(t *testing.T, client fileClient) (contract.FileService, *string, string) {
	t.Helper()
	containerRoot := t.TempDir()
	var socketPath string
	service := newFileServiceWithClientFactory(containerRoot, func(path string) fileClient {
		socketPath = path
		return client
	})
	expectedSocket := runtimeoci.SandboxdBundleSocketPath(filepath.Join(containerRoot, fileServiceTestOptions().ContainerID))
	return service, &socketPath, expectedSocket
}

func fileServiceTestOptions() contract.HandlerOptions {
	return contract.HandlerOptions{
		ContainerID: "alloc-test",
		ContainerLabels: map[string]string{
			LabelReady:        "true",
			LabelSocket:       "/tmp/sandboxd.sock",
			LabelCapabilities: "archive,file,process,pty",
		},
	}
}

type fakeFileClient struct {
	stat      FileStatResponse
	statErr   error
	list      FileListResponse
	read      FileReadResponse
	exists    FileExistsResponse
	archive   []byte
	mutations []string
}

func (f *fakeFileClient) StatFile(context.Context, string) (FileStatResponse, error) {
	return f.stat, f.statErr
}

func (f *fakeFileClient) ListDir(context.Context, string) (FileListResponse, error) {
	return f.list, nil
}

func (f *fakeFileClient) ReadFile(context.Context, string) (FileReadResponse, error) {
	return f.read, nil
}

func (f *fakeFileClient) Exists(context.Context, string) (FileExistsResponse, error) {
	return f.exists, nil
}

func (f *fakeFileClient) WriteFile(_ context.Context, _ FileWriteRequest) error {
	f.mutations = append(f.mutations, "write")
	return nil
}

func (f *fakeFileClient) Mkdir(_ context.Context, _ FileMkdirRequest) error {
	f.mutations = append(f.mutations, "mkdir")
	return nil
}

func (f *fakeFileClient) Remove(_ context.Context, _ FileRemoveRequest) error {
	f.mutations = append(f.mutations, "remove")
	return nil
}

func (f *fakeFileClient) Copy(_ context.Context, _ FileCopyRequest) error {
	f.mutations = append(f.mutations, "copy")
	return nil
}

func (f *fakeFileClient) Move(_ context.Context, _ FileMoveRequest) error {
	f.mutations = append(f.mutations, "move")
	return nil
}

func (f *fakeFileClient) Chmod(_ context.Context, _ FileChmodRequest) error {
	f.mutations = append(f.mutations, "chmod")
	return nil
}

func (f *fakeFileClient) Touch(_ context.Context, _ FileTouchRequest) error {
	f.mutations = append(f.mutations, "touch")
	return nil
}

func (f *fakeFileClient) UploadArchive(_ context.Context, _ FileArchiveUploadRequest, input io.Reader) error {
	f.mutations = append(f.mutations, "upload")
	archive, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	f.archive = archive
	return nil
}

func (f *fakeFileClient) DownloadArchive(_ context.Context, _ FileArchiveDownloadRequest, output io.Writer) error {
	f.mutations = append(f.mutations, "download")
	_, err := output.Write(f.archive)
	return err
}

func TestFileServiceUsesSandboxdForMutatingOperations(t *testing.T) {
	client := &fakeFileClient{archive: []byte("archive")}
	service, _, _ := newTestFileService(t, client)
	options := fileServiceTestOptions()

	_, err := service.WriteFile(context.Background(), &apipb.WriteFileRequest{Path: "/tmp/a", Data: []byte("ok")}, options)
	require.NoError(t, err)
	_, err = service.Mkdir(context.Background(), &apipb.MkdirRequest{Path: "/tmp/d", Parents: true}, options)
	require.NoError(t, err)
	_, err = service.Remove(context.Background(), &apipb.RemoveRequest{Path: "/tmp/a", Force: true}, options)
	require.NoError(t, err)
	_, err = service.Copy(context.Background(), &apipb.CopyRequest{SrcPath: "/tmp/a", DstPath: "/tmp/b", Overwrite: true}, options)
	require.NoError(t, err)
	_, err = service.Move(context.Background(), &apipb.MoveRequest{SrcPath: "/tmp/b", DstPath: "/tmp/c", Overwrite: true}, options)
	require.NoError(t, err)
	_, err = service.Chmod(context.Background(), &apipb.ChmodRequest{Path: "/tmp/c", Mode: 0640}, options)
	require.NoError(t, err)
	_, err = service.Touch(context.Background(), &apipb.TouchRequest{Path: "/tmp/c", Create: true}, options)
	require.NoError(t, err)
	_, err = service.UploadArchive(context.Background(), &apipb.UploadArchiveRequest{Path: "/tmp/tree"}, bytes.NewReader([]byte("upload")), options)
	require.NoError(t, err)
	var downloaded bytes.Buffer
	_, err = service.DownloadArchive(context.Background(), &apipb.DownloadArchiveRequest{Path: "/tmp/tree"}, &downloaded, options)
	require.NoError(t, err)

	assert.Equal(t, []string{"write", "mkdir", "remove", "copy", "move", "chmod", "touch", "upload", "download"}, client.mutations)
	assert.Equal(t, []byte("upload"), client.archive)
	assert.Equal(t, []byte("upload"), downloaded.Bytes())
}
