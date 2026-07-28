package bundleflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/jsonutil"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyRootfsViewToBundlePointsSpecAtWritableView(t *testing.T) {
	bundlePath := t.TempDir()
	rootfsPath := t.TempDir()
	writeRootfsViewBundleTestSpec(t, bundlePath, &spec.Spec{
		Root: &spec.Root{
			Path:     "/image/rootfs",
			Readonly: true,
		},
	})

	require.NoError(t, ApplyRootfsView(bundlePath, rootfsview.View{
		RootDir:  rootfsPath,
		Writable: true,
	}))

	ociSpec, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	require.NoError(t, err)
	require.NotNil(t, ociSpec.Root)
	assert.Equal(t, rootfsPath, ociSpec.Root.Path)
	assert.False(t, ociSpec.Root.Readonly)
}

func TestApplyRootfsViewToBundleCreatesRootSection(t *testing.T) {
	bundlePath := t.TempDir()
	rootfsPath := t.TempDir()
	writeRootfsViewBundleTestSpec(t, bundlePath, &spec.Spec{})

	require.NoError(t, ApplyRootfsView(bundlePath, rootfsview.View{
		RootDir:  rootfsPath,
		Writable: true,
	}))

	ociSpec, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	require.NoError(t, err)
	require.NotNil(t, ociSpec.Root)
	assert.Equal(t, rootfsPath, ociSpec.Root.Path)
	assert.False(t, ociSpec.Root.Readonly)
}

func TestPrepareBundleMountTargetsMaterializesTargetsAfterRootfsView(t *testing.T) {
	bundlePath := t.TempDir()
	rootfsPath := t.TempDir()
	dirSource := t.TempDir()
	fileSource := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(fileSource, []byte("secret"), 0o600))
	writeRootfsViewBundleTestSpec(t, bundlePath, &spec.Spec{
		Root: &spec.Root{Path: "/image/rootfs", Readonly: true},
		Mounts: []spec.Mount{
			{Type: "bind", Source: dirSource, Destination: "/opt/axern/agents/codex", Options: []string{"rbind", "ro"}},
			{Type: "bind", Source: fileSource, Destination: "/etc/axern/token", Options: []string{"ro"}},
		},
	})

	require.NoError(t, ApplyRootfsView(bundlePath, rootfsview.View{
		RootDir:  rootfsPath,
		Writable: true,
	}))
	require.NoError(t, PrepareBundleMountTargets(bundlePath))

	assert.DirExists(t, filepath.Join(rootfsPath, "opt", "axern", "agents", "codex"))
	assert.FileExists(t, filepath.Join(rootfsPath, "etc", "axern", "token"))
}

func TestPrepareBundleMountTargetsMaterializesTargetsWithoutRootfsView(t *testing.T) {
	bundlePath := t.TempDir()
	rootfsPath := t.TempDir()
	source := t.TempDir()
	writeRootfsViewBundleTestSpec(t, bundlePath, &spec.Spec{
		Root: &spec.Root{Path: rootfsPath},
		Mounts: []spec.Mount{{
			Type:        "bind",
			Source:      source,
			Destination: "/opt/axern/agents/codex",
			Options:     []string{"rbind", "ro"},
		}},
	})

	require.NoError(t, PrepareBundleMountTargets(bundlePath))

	assert.DirExists(t, filepath.Join(rootfsPath, "opt", "axern", "agents", "codex"))
}

func TestPrepareBundleMountTargetsRejectsBindMountTargetThroughSymlink(t *testing.T) {
	bundlePath := t.TempDir()
	rootfsPath := t.TempDir()
	source := t.TempDir()
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(rootfsPath, "opt")))
	writeRootfsViewBundleTestSpec(t, bundlePath, &spec.Spec{
		Root: &spec.Root{Path: "/image/rootfs", Readonly: true},
		Mounts: []spec.Mount{{
			Type:        "bind",
			Source:      source,
			Destination: "/opt/axern/agents/codex",
			Options:     []string{"rbind", "ro"},
		}},
	})

	require.NoError(t, ApplyRootfsView(bundlePath, rootfsview.View{
		RootDir:  rootfsPath,
		Writable: true,
	}))

	err := PrepareBundleMountTargets(bundlePath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "target path contains symlink")
}

func TestApplyRootfsViewToBundleSkipsReadonlyView(t *testing.T) {
	bundlePath := t.TempDir()
	writeRootfsViewBundleTestSpec(t, bundlePath, &spec.Spec{
		Root: &spec.Root{
			Path:     "/image/rootfs",
			Readonly: true,
		},
	})

	require.NoError(t, ApplyRootfsView(bundlePath, rootfsview.View{}))

	ociSpec, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	require.NoError(t, err)
	assert.Equal(t, "/image/rootfs", ociSpec.Root.Path)
	assert.True(t, ociSpec.Root.Readonly)
}

func TestApplyRootfsViewToBundleRejectsWritableViewWithoutRootDir(t *testing.T) {
	bundlePath := t.TempDir()
	writeRootfsViewBundleTestSpec(t, bundlePath, &spec.Spec{})

	err := ApplyRootfsView(bundlePath, rootfsview.View{Writable: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "root dir is required")
}

func writeRootfsViewBundleTestSpec(t *testing.T, bundlePath string, ociSpec *spec.Spec) {
	t.Helper()
	data, err := jsonutil.UnescapedMarshal(ociSpec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(bundlePath, config.ContainerSpecFile), data, 0644))
}
