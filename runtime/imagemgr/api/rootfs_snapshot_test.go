package api

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootfsSnapshotHTTPStatusPreservesResourceExhaustion(t *testing.T) {
	assert.Equal(t, http.StatusRequestEntityTooLarge, rootfsSnapshotHTTPStatus(&RootfsSnapshotRequestError{Err: fmt.Errorf("layer: %w", errRootfsSnapshotLayerTooLarge)}))
	assert.Equal(t, http.StatusBadRequest, rootfsSnapshotHTTPStatus(invalidRootfsSnapshotRequest("invalid contract")))
	assert.Equal(t, http.StatusInternalServerError, rootfsSnapshotHTTPStatus(errors.New("registry unavailable")))
}

func TestPrepareRootfsSnapshotStagingRemovesInterruptedFiles(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "snapshots", "staging")
	require.NoError(t, os.MkdirAll(staging, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(staging, "upper-interrupted.tar"), []byte("partial"), 0600))

	got, err := prepareRootfsSnapshotStaging(root)
	require.NoError(t, err)
	assert.Equal(t, staging, got)
	entries, err := os.ReadDir(staging)
	require.NoError(t, err)
	assert.Empty(t, entries)
	info, err := os.Stat(staging)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm())
}

func snapshotTar(t *testing.T, headers ...*tar.Header) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, header := range headers {
		require.NoError(t, writer.WriteHeader(header))
		if header.Size > 0 {
			_, err := writer.Write(bytes.Repeat([]byte{'x'}, int(header.Size)))
			require.NoError(t, err)
		}
	}
	require.NoError(t, writer.Close())
	return archive.Bytes()
}

func normalizeSnapshotTar(t *testing.T, source []byte) []tar.Header {
	t.Helper()
	var normalized bytes.Buffer
	require.NoError(t, writeOCIRootfsSnapshotLayer(bytes.NewReader(source), &normalized, 1<<20))
	reader := tar.NewReader(&normalized)
	var headers []tar.Header
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return headers
		}
		require.NoError(t, err)
		headers = append(headers, *header)
	}
}

func TestWriteOCIRootfsSnapshotLayerRejectsEscapingAndSpecialFiles(t *testing.T) {
	for _, name := range []string{"..", "../secret", "workspace/../../secret", "\\..\\secret", "/absolute"} {
		var normalized bytes.Buffer
		err := writeOCIRootfsSnapshotLayer(bytes.NewReader(snapshotTar(t, &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600})), &normalized, 1<<20)
		require.Error(t, err, name)
	}
	var normalized bytes.Buffer
	err := writeOCIRootfsSnapshotLayer(bytes.NewReader(snapshotTar(t, &tar.Header{Name: "dev/device", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3})), &normalized, 1<<20)
	require.Error(t, err)
}

func TestWriteOCIRootfsSnapshotLayerConvertsGVisorWhiteout(t *testing.T) {
	headers := normalizeSnapshotTar(t, snapshotTar(t,
		&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0755},
		&tar.Header{Name: "./workspace/", Typeflag: tar.TypeDir, Mode: 0750},
		&tar.Header{Name: "./workspace/stale", Typeflag: tar.TypeChar, Devmajor: 0, Devminor: 0, Uid: 12, Gid: 34},
	))
	require.Len(t, headers, 3)
	assert.Equal(t, "workspace/.wh.stale", headers[2].Name)
	assert.Equal(t, byte(tar.TypeReg), headers[2].Typeflag)
	assert.Equal(t, int64(0600), headers[2].Mode)
	assert.Equal(t, 12, headers[2].Uid)
	assert.Equal(t, 34, headers[2].Gid)
}

func TestWriteOCIRootfsSnapshotLayerConvertsOpaqueDirectory(t *testing.T) {
	headers := normalizeSnapshotTar(t, snapshotTar(t,
		&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0755},
		&tar.Header{Name: "./cache/", Typeflag: tar.TypeDir, Mode: 0700, PAXRecords: map[string]string{
			"SCHILY.xattr.trusted.overlay.opaque": "y",
			"SCHILY.xattr.user.example":           "value",
		}},
	))
	require.Len(t, headers, 3)
	assert.Equal(t, "cache/", headers[1].Name)
	assert.Equal(t, map[string]string{"SCHILY.xattr.user.example": "value"}, headers[1].PAXRecords)
	assert.Equal(t, "cache/.wh..wh..opq", headers[2].Name)
	assert.Equal(t, byte(tar.TypeReg), headers[2].Typeflag)
}

func TestWriteOCIRootfsSnapshotLayerPreservesFileContentAndMetadata(t *testing.T) {
	var normalized bytes.Buffer
	source := snapshotTar(t,
		&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0755},
		&tar.Header{Name: "./program", Typeflag: tar.TypeReg, Mode: 0751, Uid: 42, Gid: 43, Size: 4},
	)
	require.NoError(t, writeOCIRootfsSnapshotLayer(bytes.NewReader(source), &normalized, 1<<20))
	reader := tar.NewReader(&normalized)
	_, err := reader.Next()
	require.NoError(t, err)
	header, err := reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "program", header.Name)
	assert.Equal(t, int64(0751), header.Mode)
	assert.Equal(t, 42, header.Uid)
	assert.Equal(t, 43, header.Gid)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("xxxx"), content)
}

func TestWriteOCIRootfsSnapshotLayerEnforcesOutputLimit(t *testing.T) {
	var normalized bytes.Buffer
	err := writeOCIRootfsSnapshotLayer(bytes.NewReader(snapshotTar(t, &tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0755})), &normalized, 511)
	require.ErrorIs(t, err, errRootfsSnapshotLayerTooLarge)
}
