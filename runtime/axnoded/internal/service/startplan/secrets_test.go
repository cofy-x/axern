package startplan

import (
	"os"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaterializeResolvedSecretFilesUsesAllocationScopedPrivatePath(t *testing.T) {
	request := &runtime.StartRequest{
		AllocationID: "alloc/../../unsafe",
		SecretFiles: []*runtime.ResolvedSecretFile{{
			Path:    "/run/secrets/token",
			Content: []byte("secret"),
			Mode:    0o400,
		}},
	}
	mounts, cleanup, err := MaterializeResolvedSecretFiles(request)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.Len(t, mounts, 1)
	assert.Equal(t, "/run/secrets/token", mounts[0].GetTarget())
	content, err := os.ReadFile(mounts[0].GetSource())
	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), content)
	assert.NotContains(t, mounts[0].GetSource(), request.GetAllocationID())

	cleanup()
	_, err = os.Stat(mounts[0].GetSource())
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestCleanupResolvedSecretFilesUsesMaterializationPath(t *testing.T) {
	request := &runtime.StartRequest{
		AllocationID: "alloc-cleanup",
		SecretFiles: []*runtime.ResolvedSecretFile{{
			Path:    "/run/secrets/token",
			Content: []byte("sensitive"),
		}},
	}
	mounts, _, err := MaterializeResolvedSecretFiles(request)
	if err != nil {
		t.Fatalf("MaterializeResolvedSecretFiles() error = %v", err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mount count = %d, want 1", len(mounts))
	}
	if err := CleanupResolvedSecretFiles(request.GetAllocationID()); err != nil {
		t.Fatalf("CleanupResolvedSecretFiles() error = %v", err)
	}
	if _, err := os.Stat(mounts[0].GetSource()); !os.IsNotExist(err) {
		t.Fatalf("secret source still exists after cleanup: %v", err)
	}
}

func TestMaterializeResolvedSecretFilesRejectsUnsafeOrDuplicateTargets(t *testing.T) {
	for _, files := range [][]*runtime.ResolvedSecretFile{
		{{Path: "../token", Content: []byte("secret")}},
		{{Path: "/token"}, {Path: "/token"}},
		{nil},
	} {
		_, _, err := MaterializeResolvedSecretFiles(&runtime.StartRequest{AllocationID: "alloc-safe", SecretFiles: files})
		assert.Error(t, err)
	}
}
