package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunscOverlayUsesBoundedFileBacking(t *testing.T) {
	handler := &RunscServiceHandler{filestoreDir: "/var/lib/axnoded/filestore", writableLayerLimitBytes: 256 << 20}
	value, err := handler.overlay2Value(false, 256<<20)
	require.NoError(t, err)
	assert.Equal(t, "root:dir=/var/lib/axnoded/filestore/runsc,size=268435456", value)
}

func TestRunscOverlayRejectsMissingFilestore(t *testing.T) {
	_, err := (&RunscServiceHandler{writableLayerLimitBytes: 256 << 20}).overlay2Value(false, 256<<20)
	require.ErrorContains(t, err, "filestore_dir")
}

func TestRunscOverlayRejectsMissingLimit(t *testing.T) {
	_, err := (&RunscServiceHandler{filestoreDir: "/filestore"}).overlay2Value(false, 0)
	require.ErrorContains(t, err, "writable_layer_limit_bytes")
}

func TestRunscOverlaySkipsReadonlyRoot(t *testing.T) {
	value, err := (&RunscServiceHandler{}).overlay2Value(true, 0)
	require.NoError(t, err)
	assert.Empty(t, value)
}

func TestRunscOverlayArgsUsesBoundedFilestore(t *testing.T) {
	bundlePath := t.TempDir()
	writeRunscOverlayTestSpec(t, bundlePath, spec.Root{Path: t.TempDir()})
	handler := &RunscServiceHandler{filestoreDir: "/var/lib/axnoded/filestore", writableLayerLimitBytes: 1024}
	args, err := handler.overlayArgsForBundle(bundlePath, 256<<20)
	require.NoError(t, err)
	assert.Equal(t, []string{"--overlay2", "root:dir=/var/lib/axnoded/filestore/runsc,size=268435456"}, args)
}

func TestRunscOverlayArgsSkipsReadonlyBundleRoot(t *testing.T) {
	bundlePath := t.TempDir()
	writeRunscOverlayTestSpec(t, bundlePath, spec.Root{Path: t.TempDir(), Readonly: true})
	args, err := (&RunscServiceHandler{}).overlayArgsForBundle(bundlePath, 0)
	require.NoError(t, err)
	assert.Nil(t, args)
}

func writeRunscOverlayTestSpec(t *testing.T, bundlePath string, root spec.Root) {
	t.Helper()
	data, err := json.Marshal(spec.Spec{Root: &root})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(bundlePath, config.ContainerSpecFile), data, 0644))
}
