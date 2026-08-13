package rootfsview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverlayProviderRejectsUnsafeContainerID(t *testing.T) {
	provider := NewOverlayProvider(t.TempDir())

	_, err := provider.Prepare(context.Background(), "../escape", Request{})
	require.ErrorContains(t, err, "invalid container ID")
	require.ErrorContains(t, provider.Remove(context.Background(), "../escape"), "invalid container ID")
	require.ErrorContains(t, provider.Remove(context.Background(), ""), "invalid container ID")
	_, err = provider.Prepare(context.Background(), "escape;command", Request{})
	require.ErrorContains(t, err, "invalid container ID")
}

func TestOverlayViewForContainerUsesFilestore(t *testing.T) {
	view := overlayViewForContainer("alloc-1", "/var/lib/axnoded/filestore", runcViewDir, []string{"/image/rootfs"})

	assert.Equal(t, []string{"/image/rootfs"}, view.LowerDirs)
	assert.Equal(t, "/var/lib/axnoded/filestore/runc/alloc-1/upper", view.UpperDir)
	assert.Equal(t, "/var/lib/axnoded/filestore/runc/alloc-1/work", view.WorkDir)
	assert.Equal(t, "/var/lib/axnoded/filestore/runc/alloc-1/merged", view.MergedDir)
}

func TestOverlayViewInitializationNeverReplacesExistingView(t *testing.T) {
	view := overlayViewForContainer("sandbox", t.TempDir(), projectionViewDir, []string{"/lower"})
	require.NoError(t, initializeOverlayView(view))
	require.ErrorContains(t, initializeOverlayView(view), "already exists")
	require.NoError(t, cleanupOverlayView(filepath.Dir(view.MergedDir)))
	require.NoError(t, cleanupOverlayView(filepath.Dir(view.MergedDir)))
}

func TestOverlayViewInitializationRollsBackPartialView(t *testing.T) {
	view := overlayViewForContainer("sandbox", t.TempDir(), projectionViewDir, []string{"/lower"})
	calls := 0
	err := initializeOverlayViewWithMkdir(view, func(name string, mode os.FileMode) error {
		calls++
		if calls == 3 {
			return errors.New("injected mkdir failure")
		}
		return os.Mkdir(name, mode)
	})
	require.ErrorContains(t, err, "injected mkdir failure")
	_, statErr := os.Stat(filepath.Dir(view.MergedDir))
	require.ErrorIs(t, statErr, os.ErrNotExist)
	require.NoError(t, initializeOverlayView(view))
}

func TestImmutableMountDescriptorIsSourceOwnedAndBounded(t *testing.T) {
	root := t.TempDir()
	backing, err := InspectBacking(root)
	require.NoError(t, err)
	descriptor := backing.ImmutableMountDescriptor("")
	require.NoError(t, ValidateImmutableMountDescriptor(descriptor, root))
	descriptor.Identity = "derived-v1"
	require.ErrorContains(t, ValidateImmutableMountDescriptor(descriptor, root), "sha256")

	descriptor = backing.ImmutableMountDescriptor("")
	descriptor.LowerDirs = make([]string, maxImmutableLowerDirs+1)
	require.ErrorContains(t, ValidateImmutableMountDescriptorContract(descriptor, root), "too many lower dirs")
}

func TestImmutableMountDescriptorContractRejectsDuplicateAndMalformedState(t *testing.T) {
	root := t.TempDir()
	descriptor := ImmutableMountDescriptor{
		Identity: "sha256:" + strings.Repeat("a", 64), EffectiveRoot: root, Filesystem: "overlay",
		BackingFilesystems: []string{"erofs"}, LowerDirs: []string{root}, Readonly: true, LeaseID: "lease-1",
	}
	require.NoError(t, ValidateImmutableMountDescriptorContract(descriptor, root))
	descriptor.LowerDirs = []string{root, root}
	require.ErrorContains(t, ValidateImmutableMountDescriptorContract(descriptor, root), "duplicated")
	descriptor.LowerDirs = []string{root}
	descriptor.Identity = "sha256:" + strings.Repeat("g", 64)
	require.ErrorContains(t, ValidateImmutableMountDescriptorContract(descriptor, root), "sha256")

	descriptor.Identity = "sha256:" + strings.Repeat("a", 64)
	descriptor.BackingFilesystems = []string{"xfs", "erofs"}
	require.ErrorContains(t, ValidateImmutableMountDescriptorContract(descriptor, root), "ordered")
	descriptor.BackingFilesystems = []string{"erofs", "xfs"}
	require.ErrorContains(t, ValidateImmutableMountDescriptorContract(descriptor, root+"/../"+filepath.Base(root)), "canonical")
	descriptor.LeaseID = " lease-1"
	require.ErrorContains(t, ValidateImmutableMountDescriptorContract(descriptor, root), "lease")
}

func TestRootfsBackingFactsHasFilesystemIncludesEffectiveLowerChain(t *testing.T) {
	facts := RootfsBackingFacts{
		FSType: "overlay",
		EffectiveLowerChain: []RootfsBackingLayerFacts{
			{Path: "/upper", FSType: "xfs"},
			{Path: "/image", FSType: "erofs"},
		},
	}
	assert.True(t, facts.HasFilesystem("EROFS"))
	assert.False(t, facts.HasFilesystem("ext4"))
}

func TestInspectMountTargetsFindsMissingTargets(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "etc"), 0750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc", "hostname"), nil, 0644))

	missing, err := inspectMountTargets(root, []MountTarget{
		{Destination: "/etc/hostname", Kind: TargetRegularFile},
		{Destination: "/mnt", Kind: TargetDirectory},
	})
	require.NoError(t, err)
	require.Len(t, missing, 1)
	assert.Equal(t, "/mnt", missing[0].Destination)
}

func TestInspectMountTargetsRejectsNonCanonicalDestinations(t *testing.T) {
	root := t.TempDir()
	for _, destination := range []string{"", " /etc/hosts", "/etc/hosts ", "//etc/hosts", "/etc/../hosts", "/etc/./hosts", "/etc/hosts/"} {
		t.Run(destination, func(t *testing.T) {
			_, err := inspectMountTargets(root, []MountTarget{{Destination: destination, Kind: TargetRegularFile}})
			require.ErrorContains(t, err, "canonical")
		})
	}
}

func TestInspectMountTargetsRejectsSymlinkAndTypeMismatch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "real"), 0755))
	require.NoError(t, os.Symlink("real", filepath.Join(root, "link")))

	_, err := inspectMountTargets(root, []MountTarget{{Destination: "/link/file", Kind: TargetRegularFile}})
	require.ErrorContains(t, err, "symlink")

	_, err = inspectMountTargets(root, []MountTarget{{Destination: "/real", Kind: TargetRegularFile}})
	require.ErrorContains(t, err, "not a regular file")
}

func TestInspectMountTargetsRejectsRelativeAndSpecialLeaf(t *testing.T) {
	root := t.TempDir()
	_, err := inspectMountTargets(root, []MountTarget{{Destination: "etc/hosts", Kind: TargetRegularFile}})
	require.ErrorContains(t, err, "absolute")

	require.NoError(t, os.Symlink("missing", filepath.Join(root, "hosts")))
	_, err = inspectMountTargets(root, []MountTarget{{Destination: "/hosts", Kind: TargetRegularFile}})
	require.ErrorContains(t, err, "symlink")
}

func TestSeedMountTargetsCopiesExistingParentMetadata(t *testing.T) {
	lower := t.TempDir()
	upper := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(lower, "etc"), 0710))

	require.NoError(t, seedMountTargets(lower, upper, []MountTarget{{Destination: "/etc/hosts", Kind: TargetRegularFile}}))
	parent, err := os.Stat(filepath.Join(upper, "etc"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0710), parent.Mode().Perm())
	leaf, err := os.Stat(filepath.Join(upper, "etc", "hosts"))
	require.NoError(t, err)
	assert.True(t, leaf.Mode().IsRegular())
}

func TestSeedSymlinksCreatesPublicClaudeCodePathInProjection(t *testing.T) {
	lower := t.TempDir()
	upper := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(lower, "opt", "axern", "agents"), 0755))

	require.NoError(t, seedSymlinks(lower, upper, []Symlink{{
		Path: "/opt/axern/agents/claude-code", Target: "/__claude_code",
	}}))
	target, err := os.Readlink(filepath.Join(upper, "opt", "axern", "agents", "claude-code"))
	require.NoError(t, err)
	assert.Equal(t, "/__claude_code", target)
}

func TestSeedSymlinksRejectsOccupiedPublicPath(t *testing.T) {
	lower := t.TempDir()
	upper := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(lower, "opt", "axern", "agents", "claude-code"), 0755))

	err := seedSymlinks(lower, upper, []Symlink{{
		Path: "/opt/axern/agents/claude-code", Target: "/__claude_code",
	}})
	require.ErrorContains(t, err, "already occupied")
}

func TestReconcilePersistentViewsRemovesOnlyStaleRuntimeOwnedView(t *testing.T) {
	filestore := t.TempDir()
	provider := NewOverlayProvider(filestore).(*overlayProvider)
	for _, item := range []struct {
		id      string
		runtime string
	}{
		{id: "stale-runc", runtime: "runc"},
		{id: "active-runc", runtime: "runc"},
		{id: "stale-runsc", runtime: "runsc"},
	} {
		root := filepath.Join(filestore, projectionViewDir, item.id)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "merged"), 0755))
		content := []byte(`{"runtime_name":"` + item.runtime + `","immutable_mount":{"identity":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","effective_root":"/imagemgr/rootfs","filesystem":"overlay","lower_dirs":["/imagemgr/lower"],"readonly":true}}`)
		require.NoError(t, atomicWrite(filepath.Join(root, "projection.json"), content, 0644))
	}

	err := provider.ReconcilePersistentViews(context.Background(), "runc", map[string]struct{}{"active-runc": {}})
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(filestore, projectionViewDir, "stale-runc"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(filestore, projectionViewDir, "active-runc"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(filestore, projectionViewDir, "stale-runsc"))
	require.NoError(t, err)
}

func TestReadProjectionManifestRejectsUnboundedInput(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "projection.json"), make([]byte, maxProjectionManifestBytes+1), 0644))
	_, err := readProjectionManifest(root)
	require.ErrorContains(t, err, "size")
}

func TestReadProjectionManifestRejectsUnknownOrTrailingState(t *testing.T) {
	for name, content := range map[string]string{
		"unknown":  `{"runtime_name":"runc","unknown":true,"immutable_mount":{"identity":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","effective_root":"/rootfs","filesystem":"overlay","lower_dirs":["/lower"],"readonly":true}}`,
		"trailing": `{"runtime_name":"runc","immutable_mount":{"identity":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","effective_root":"/rootfs","filesystem":"overlay","lower_dirs":["/lower"],"readonly":true}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "projection.json"), []byte(content), 0644))
			_, err := readProjectionManifest(root)
			require.Error(t, err)
		})
	}
}
