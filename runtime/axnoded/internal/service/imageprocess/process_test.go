package imageprocess

import (
	"os"
	"path/filepath"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveMountsRequiresHostBackedMount(t *testing.T) {
	_, err := ResolveMounts(&specs.Spec{}, []*apipb.ImageProcessMount{{
		SandboxPath: "/workspace",
		TargetPath:  "/workspace",
	}})
	assert.ErrorIs(t, err, errord.ErrFailedPrecondition)
}

func TestResolveMountsRejectsTraversalAndRelativePaths(t *testing.T) {
	hostDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(hostDir, "project"), 0755))
	targetSpec := &specs.Spec{Mounts: []specs.Mount{{
		Type:        "bind",
		Source:      hostDir,
		Destination: "/workspace",
	}}}

	cases := []*apipb.ImageProcessMount{
		{SandboxPath: "workspace", TargetPath: "/workspace"},
		{SandboxPath: "/workspace/../root", TargetPath: "/workspace"},
		{SandboxPath: "/workspace", TargetPath: "workspace"},
		{SandboxPath: "/workspace", TargetPath: "/workspace/../root"},
	}
	for _, tc := range cases {
		_, err := ResolveMounts(targetSpec, []*apipb.ImageProcessMount{tc})
		assert.ErrorIs(t, err, errord.ErrInvalidArgument)
	}
}

func TestResolveMountsMapsSandboxPathUnderHostBackedMount(t *testing.T) {
	hostDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(hostDir, "project"), 0755))
	targetSpec := &specs.Spec{Mounts: []specs.Mount{{
		Type:        "bind",
		Source:      hostDir,
		Destination: "/workspace",
	}}}

	mounts, err := ResolveMounts(targetSpec, []*apipb.ImageProcessMount{{
		SandboxPath: "/workspace/project",
		TargetPath:  "/workspace",
		Readonly:    true,
		Options:     []string{"rw", "nodev"},
	}})

	require.NoError(t, err)
	require.Len(t, mounts, 1)
	assert.Equal(t, "bind", mounts[0].GetType())
	assert.Equal(t, filepath.Join(hostDir, "project"), mounts[0].GetSource())
	assert.Equal(t, "/workspace", mounts[0].GetTarget())
	assert.Contains(t, mounts[0].GetOptions(), "rbind")
	assert.Contains(t, mounts[0].GetOptions(), "ro")
	assert.Contains(t, mounts[0].GetOptions(), "nodev")
	assert.NotContains(t, mounts[0].GetOptions(), "rw")
}

func TestEnsureMountTargetsPreparesMissingDirectoryAndFileTargets(t *testing.T) {
	rootfsDir := t.TempDir()
	sourceDir := t.TempDir()
	sourceFile := filepath.Join(t.TempDir(), "input.txt")
	require.NoError(t, os.WriteFile(sourceFile, []byte("input"), 0644))

	err := EnsureMountTargets(rootfsDir, []*apipb.Mount{
		{Type: "bind", Source: sourceDir, Target: "/workspace"},
		{Type: "bind", Source: sourceFile, Target: "/tmp/input.txt"},
	})

	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(rootfsDir, "workspace"))
	assert.FileExists(t, filepath.Join(rootfsDir, "tmp", "input.txt"))
}

func TestEnsureMountTargetsRejectsTargetTypeConflict(t *testing.T) {
	rootfsDir := t.TempDir()
	sourceDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(rootfsDir, "workspace"), []byte("not-dir"), 0644))

	err := EnsureMountTargets(rootfsDir, []*apipb.Mount{{
		Type:   "bind",
		Source: sourceDir,
		Target: "/workspace",
	}})

	assert.ErrorIs(t, err, errord.ErrFailedPrecondition)
}

func TestEnsureMountTargetsRejectsActorRootfsSymlinkTargets(t *testing.T) {
	rootfsDir := t.TempDir()
	sourceFile := filepath.Join(t.TempDir(), "input.txt")
	require.NoError(t, os.WriteFile(sourceFile, []byte("input"), 0644))
	require.NoError(t, os.Symlink("/tmp/escape", filepath.Join(rootfsDir, "input.txt")))

	err := EnsureMountTargets(rootfsDir, []*apipb.Mount{{
		Type:   "bind",
		Source: sourceFile,
		Target: "/input.txt",
	}})

	assert.ErrorIs(t, err, errord.ErrFailedPrecondition)
}

func TestEnsureMountTargetsRejectsActorRootfsSymlinkParents(t *testing.T) {
	rootfsDir := t.TempDir()
	sourceFile := filepath.Join(t.TempDir(), "input.txt")
	require.NoError(t, os.WriteFile(sourceFile, []byte("input"), 0644))
	require.NoError(t, os.Symlink("/tmp/escape", filepath.Join(rootfsDir, "tmp")))

	err := EnsureMountTargets(rootfsDir, []*apipb.Mount{{
		Type:   "bind",
		Source: sourceFile,
		Target: "/tmp/input.txt",
	}})

	assert.ErrorIs(t, err, errord.ErrFailedPrecondition)
}
