package service

import (
	"bytes"
	"context"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileOperationsBridgeToRuntimeFileService(t *testing.T) {
	handler := &runtimeSpyHandler{
		name: "runsc",
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": handler,
	})
	storeRunningExecContainer(t, s, "runsc", "axctl-file-bridge")

	readResp, err := s.ReadFile(context.Background(), &runtime.ReadFileRequest{
		ID:   "axctl-file-bridge",
		Path: "/tmp/out.txt",
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), readResp.GetData())

	_, err = s.WriteFile(context.Background(), &runtime.WriteFileRequest{
		ID:            "axctl-file-bridge",
		Path:          "/tmp/out.txt",
		Data:          []byte("hello"),
		CreateParents: true,
	})
	require.NoError(t, err)

	_, err = s.Mkdir(context.Background(), &runtime.MkdirRequest{
		ID:      "axctl-file-bridge",
		Path:    "/tmp/nested",
		Parents: true,
	})
	require.NoError(t, err)

	_, err = s.Remove(context.Background(), &runtime.RemoveRequest{
		ID:        "axctl-file-bridge",
		Path:      "/tmp/nested",
		Recursive: true,
		Force:     true,
	})
	require.NoError(t, err)

	existsResp, err := s.Exists(context.Background(), &runtime.ExistsRequest{
		ID:   "axctl-file-bridge",
		Path: "/tmp/out.txt",
	})
	require.NoError(t, err)
	assert.True(t, existsResp.GetExists())

	_, err = s.Copy(context.Background(), &runtime.CopyRequest{
		ID:        "axctl-file-bridge",
		SrcPath:   "/tmp/out.txt",
		DstPath:   "/tmp/copy.txt",
		Recursive: true,
		Overwrite: true,
	})
	require.NoError(t, err)

	_, err = s.Move(context.Background(), &runtime.MoveRequest{
		ID:        "axctl-file-bridge",
		SrcPath:   "/tmp/copy.txt",
		DstPath:   "/tmp/moved.txt",
		Overwrite: true,
	})
	require.NoError(t, err)

	_, err = s.Chmod(context.Background(), &runtime.ChmodRequest{
		ID:        "axctl-file-bridge",
		Path:      "/tmp/moved.txt",
		Mode:      0600,
		Recursive: true,
	})
	require.NoError(t, err)

	_, err = s.Touch(context.Background(), &runtime.TouchRequest{
		ID:      "axctl-file-bridge",
		Path:    "/tmp/moved.txt",
		Create:  true,
		MtimeNs: 7,
	})
	require.NoError(t, err)

	assert.Equal(t, "/tmp/out.txt", handler.readFileRequests[0].GetPath())
	assert.Equal(t, []byte("hello"), handler.writeFileRequests[0].GetData())
	assert.True(t, handler.writeFileRequests[0].GetCreateParents())
	assert.True(t, handler.mkdirRequests[0].GetParents())
	assert.True(t, handler.removeRequests[0].GetRecursive())
	assert.True(t, handler.removeRequests[0].GetForce())
	assert.Equal(t, "/tmp/out.txt", handler.existsRequests[0].GetPath())
	assert.Equal(t, "/tmp/copy.txt", handler.copyRequests[0].GetDstPath())
	assert.Equal(t, "/tmp/moved.txt", handler.moveRequests[0].GetDstPath())
	assert.Equal(t, uint32(0600), handler.chmodRequests[0].GetMode())
	assert.Equal(t, int64(7), handler.touchRequests[0].GetMtimeNs())
	require.NotEmpty(t, handler.fileOptions)
	assert.Equal(t, "axctl-file-bridge", handler.fileOptions[0].ContainerID)
	assert.Equal(t, "true", handler.fileOptions[0].ContainerLabels[runtimesandboxd.LabelReady])
	assert.Contains(t, handler.fileOptions[0].ContainerLabels[runtimesandboxd.LabelCapabilities], "file")
}

func TestArchiveOperationsBridgeToRuntimeFileService(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	storeRunningExecContainer(t, s, "runsc", "axctl-archive-bridge")

	_, err := s.UploadArchive(context.Background(), &runtime.UploadArchiveRequest{
		ID:            "axctl-archive-bridge",
		Path:          "/tmp/tree",
		CreateParents: true,
		Overwrite:     true,
	}, bytes.NewReader([]byte("archive")))
	require.NoError(t, err)

	var output bytes.Buffer
	_, err = s.DownloadArchive(context.Background(), &runtime.DownloadArchiveRequest{
		ID:   "axctl-archive-bridge",
		Path: "/tmp/tree",
	}, &output)
	require.NoError(t, err)

	require.Len(t, handler.uploadRequests, 1)
	assert.Equal(t, "/tmp/tree", handler.uploadRequests[0].GetPath())
	require.Len(t, handler.downloadRequests, 1)
	assert.Equal(t, "/tmp/tree", handler.downloadRequests[0].GetPath())
	assert.Equal(t, "archive", output.String())
	require.Len(t, handler.fileOptions, 2)
	assert.Equal(t, "axctl-archive-bridge", handler.fileOptions[0].ContainerID)
	assert.Contains(t, handler.fileOptions[0].ContainerLabels[runtimesandboxd.LabelCapabilities], "archive")
}
