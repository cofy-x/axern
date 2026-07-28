package fileapi

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceReadOnlyOperations(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "message.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("hello"), 0640))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0755))

	service := NewService()

	stat, err := service.Stat(filePath)
	require.NoError(t, err)
	assert.Equal(t, filePath, stat.Info.GetPath())
	assert.Equal(t, filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE, stat.Info.GetKind())
	assert.Equal(t, int64(5), stat.Info.GetSize())
	assert.Equal(t, uint32(0640), stat.Info.GetMode())

	read, err := service.Read(filePath)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), read.Data)

	exists, err := service.Exists(filePath)
	require.NoError(t, err)
	assert.True(t, exists.Exists)

	missing, err := service.Exists(filepath.Join(dir, "missing"))
	require.NoError(t, err)
	assert.False(t, missing.Exists)

	list, err := service.List(dir)
	require.NoError(t, err)
	require.Len(t, list.Entries, 2)
	assert.Equal(t, filePath, list.Entries[0].GetPath())
	assert.Equal(t, filepath.Join(dir, "subdir"), list.Entries[1].GetPath())
	assert.Equal(t, filev1.SandboxFileKind_SANDBOX_FILE_KIND_DIRECTORY, list.Entries[1].GetKind())
}

func TestServiceClassifiesMissingAndInvalidPaths(t *testing.T) {
	service := NewService()

	_, err := service.Stat(filepath.Join(t.TempDir(), "missing"))
	assert.True(t, errord.IsNotFound(err))
	assert.Equal(t, 404, StatusCode(err))

	_, err = service.Read("")
	assert.True(t, errord.IsInvalidArgument(err))
	assert.Equal(t, 400, StatusCode(err))
}

func TestServiceMutatingOperations(t *testing.T) {
	dir := t.TempDir()
	service := NewService()

	require.NoError(t, service.Write(WriteRequest{Path: filepath.Join(dir, "nested", "message.txt"), Data: []byte("hello"), CreateParents: true}))
	read, err := service.Read(filepath.Join(dir, "nested", "message.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), read.Data)

	require.NoError(t, service.Mkdir(MkdirRequest{Path: filepath.Join(dir, "dst"), Parents: true}))
	require.NoError(t, service.Copy(CopyRequest{
		SrcPath:   filepath.Join(dir, "nested", "message.txt"),
		DstPath:   filepath.Join(dir, "dst", "copied.txt"),
		Overwrite: true,
	}))
	require.NoError(t, service.Move(MoveRequest{
		SrcPath:   filepath.Join(dir, "dst", "copied.txt"),
		DstPath:   filepath.Join(dir, "dst", "moved.txt"),
		Overwrite: true,
	}))
	require.NoError(t, service.Chmod(ChmodRequest{Path: filepath.Join(dir, "dst", "moved.txt"), Mode: 0600}))
	stat, err := service.Stat(filepath.Join(dir, "dst", "moved.txt"))
	require.NoError(t, err)
	assert.Equal(t, uint32(0600), stat.Info.GetMode())

	require.NoError(t, service.Touch(TouchRequest{Path: filepath.Join(dir, "dst", "touched.txt"), Create: true, MtimeNs: 1710000000123456789}))
	touched, err := service.Stat(filepath.Join(dir, "dst", "touched.txt"))
	require.NoError(t, err)
	assert.Equal(t, int64(1710000000123456789), touched.Info.GetMtimeNs())

	require.NoError(t, service.Remove(RemoveRequest{Path: filepath.Join(dir, "dst"), Recursive: true}))
	exists, err := service.Exists(filepath.Join(dir, "dst", "moved.txt"))
	require.NoError(t, err)
	assert.False(t, exists.Exists)
}

func TestServiceArchiveUploadDownload(t *testing.T) {
	dir := localTempDir(t)
	service := NewService()
	archive := testTar(t, map[string]string{"nested/data.txt": "data"})

	require.NoError(t, service.UploadArchive(UploadArchiveOptions{
		Path:          filepath.Join(dir, "tree"),
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		CreateParents: true,
		Overwrite:     true,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, bytes.NewReader(archive)))

	read, err := service.Read(filepath.Join(dir, "tree", "nested", "data.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), read.Data)

	var downloaded bytes.Buffer
	require.NoError(t, service.DownloadArchive(DownloadArchiveOptions{
		Path:          filepath.Join(dir, "tree"),
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, &downloaded))
	assert.Contains(t, tarNames(t, downloaded.Bytes()), "nested/data.txt")
}

func TestServiceArchivePreservesLargeFiles(t *testing.T) {
	dir := localTempDir(t)
	service := NewService()
	data := strings.Repeat("large-data-", 128*1024)
	archive := testTar(t, map[string]string{"large.txt": data})

	require.NoError(t, service.UploadArchive(UploadArchiveOptions{
		Path:          filepath.Join(dir, "tree"),
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		CreateParents: true,
		Overwrite:     true,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, bytes.NewReader(archive)))

	read, err := service.Read(filepath.Join(dir, "tree", "large.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte(data), read.Data)
}

func TestServiceArchiveRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
	}{
		{
			name:    "symlink",
			archive: testTarWithHeader(t, &tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "target"}),
		},
		{
			name:    "hardlink",
			archive: testTarWithHeader(t, &tar.Header{Name: "link", Typeflag: tar.TypeLink, Linkname: "target"}),
		},
		{
			name:    "absolute",
			archive: testTar(t, map[string]string{"/absolute.txt": "data"}),
		},
		{
			name:    "escape",
			archive: testTar(t, map[string]string{"../escape.txt": "data"}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService()
			err := service.UploadArchive(UploadArchiveOptions{
				Path:          filepath.Join(localTempDir(t), "tree"),
				Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
				CreateParents: true,
				Overwrite:     true,
				SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
			}, bytes.NewReader(tt.archive))

			assert.True(t, errord.IsInvalidArgument(err))
		})
	}
}

func TestServiceArchiveRejectsUnsafeUploadBeforeCreatingTarget(t *testing.T) {
	dir := localTempDir(t)
	target := filepath.Join(dir, "tree")
	service := NewService()

	err := service.UploadArchive(UploadArchiveOptions{
		Path:          target,
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		CreateParents: true,
		Overwrite:     true,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, bytes.NewReader(testTar(t, map[string]string{"../escape.txt": "data"})))

	assert.True(t, errord.IsInvalidArgument(err))
	_, statErr := os.Lstat(target)
	assert.True(t, os.IsNotExist(statErr), "target should not be created for invalid archives")
}

func TestServiceArchiveRejectsResourceLimitViolations(t *testing.T) {
	deepName := strings.Repeat("nested/", maxArchivePathDepth+1) + "data.txt"
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "entry size",
			data: testTarHeaderOnly(t, &tar.Header{Name: "large.txt", Typeflag: tar.TypeReg, Mode: 0644, Size: maxArchiveEntryBytes + 1}),
		},
		{
			name: "path depth",
			data: testTar(t, map[string]string{deepName: "data"}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService()
			err := service.UploadArchive(UploadArchiveOptions{
				Path:          filepath.Join(localTempDir(t), "tree"),
				Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
				CreateParents: true,
				Overwrite:     true,
				SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
			}, bytes.NewReader(tt.data))

			assert.Error(t, err)
		})
	}
}

func TestServiceArchiveRejectsSymlinkedTargetTrees(t *testing.T) {
	dir := localTempDir(t)
	service := NewService()
	target := filepath.Join(dir, "target")
	tree := filepath.Join(dir, "tree")
	require.NoError(t, os.MkdirAll(filepath.Join(tree, "nested"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tree, "nested", "data.txt"), []byte("data"), 0644))
	require.NoError(t, os.Symlink(target, filepath.Join(tree, "nested", "link")))

	err := service.UploadArchive(UploadArchiveOptions{
		Path:          tree,
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		CreateParents: true,
		Overwrite:     true,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, bytes.NewReader(testTar(t, map[string]string{"replacement.txt": "data"})))
	assert.True(t, errord.IsFailedPrecondition(err))

	var downloaded bytes.Buffer
	err = service.DownloadArchive(DownloadArchiveOptions{
		Path:          tree,
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, &downloaded)
	assert.True(t, errord.IsFailedPrecondition(err))
}

func localTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", "fileapi-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	return abs
}

func testTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0755}))
	for name, data := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(data))}))
		_, err := tw.Write([]byte(data))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

func testTarWithHeader(t *testing.T, header *tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(header))
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

func testTarHeaderOnly(t *testing.T, header *tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(header))
	return buf.Bytes()
}

func tarNames(t *testing.T, data []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return names
		}
		require.NoError(t, err)
		names = append(names, header.Name)
	}
}
