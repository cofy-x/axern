package rootfsflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type providerStub struct {
	request rootfsview.Request
	view    rootfsview.View
	err     error
	removed bool
}

func (p *providerStub) Prepare(_ context.Context, _ string, request rootfsview.Request) (rootfsview.View, error) {
	p.request = request
	return p.view, p.err
}

func (p *providerStub) Remove(context.Context, string) error {
	p.removed = true
	return nil
}

func TestPrepareBundleAppliesProjectionAndPreservesReadonly(t *testing.T) {
	bundlePath := t.TempDir()
	rootfsPath := t.TempDir()
	projectedPath := t.TempDir()
	fileSource := filepath.Join(t.TempDir(), "hosts")
	require.NoError(t, os.WriteFile(fileSource, nil, 0644))
	require.NoError(t, runtimeoci.WriteSpecAtomic(filepath.Join(bundlePath, config.ContainerSpecFile), &spec.Spec{
		Root: &spec.Root{Path: rootfsPath, Readonly: true},
		Mounts: []spec.Mount{{
			Type: "bind", Source: fileSource, Destination: "/etc/hosts",
		}},
	}))
	provider := &providerStub{view: rootfsview.View{RootDir: projectedPath, Prepared: true}}

	prepared, err := PrepareBundle(context.Background(), provider, contract.HandlerOptions{ContainerID: "alloc-1"}, bundlePath, RuntimePolicy{RuntimeName: "runsc", ImmutableMount: testImmutableMount(rootfsPath)})

	require.NoError(t, err)
	assert.True(t, prepared)
	assert.Equal(t, rootfsPath, provider.request.RootDir)
	assert.True(t, provider.request.Readonly)
	assert.Equal(t, "runsc", provider.request.RuntimeName)
	require.Len(t, provider.request.Targets, 1)
	assert.Equal(t, rootfsview.TargetRegularFile, provider.request.Targets[0].Kind)
	written, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	require.NoError(t, err)
	assert.Equal(t, projectedPath, written.Root.Path)
	assert.True(t, written.Root.Readonly)
}

func TestPrepareBundleLeavesSpecWhenProjectionIsNotNeeded(t *testing.T) {
	bundlePath := t.TempDir()
	rootfsPath := t.TempDir()
	require.NoError(t, runtimeoci.WriteSpecAtomic(filepath.Join(bundlePath, config.ContainerSpecFile), &spec.Spec{
		Root: &spec.Root{Path: rootfsPath, Readonly: true},
	}))
	provider := &providerStub{}

	prepared, err := PrepareBundle(context.Background(), provider, contract.HandlerOptions{ContainerID: "alloc-1"}, bundlePath, RuntimePolicy{ImmutableMount: testImmutableMount(rootfsPath)})

	require.NoError(t, err)
	assert.False(t, prepared)
	written, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	require.NoError(t, err)
	assert.Equal(t, rootfsPath, written.Root.Path)
}

func TestPrepareBundleRejectsSpecialBindSource(t *testing.T) {
	bundlePath := t.TempDir()
	rootfsPath := t.TempDir()
	pipe := filepath.Join(t.TempDir(), "pipe")
	require.NoError(t, os.MkdirAll(filepath.Dir(pipe), 0755))
	require.NoError(t, syscallMkfifo(pipe, 0600))
	require.NoError(t, runtimeoci.WriteSpecAtomic(filepath.Join(bundlePath, config.ContainerSpecFile), &spec.Spec{
		Root:   &spec.Root{Path: rootfsPath},
		Mounts: []spec.Mount{{Type: "bind", Source: pipe, Destination: "/pipe"}},
	}))

	_, err := PrepareBundle(context.Background(), &providerStub{}, contract.HandlerOptions{ContainerID: "alloc-1"}, bundlePath, RuntimePolicy{ImmutableMount: testImmutableMount(rootfsPath)})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "neither a regular file nor a directory")
}

func testImmutableMount(root string) rootfsview.ImmutableMountDescriptor {
	return rootfsview.ImmutableMountDescriptor{
		Identity: "sha256:" + strings.Repeat("a", 64), EffectiveRoot: root, Filesystem: "test",
		LowerDirs: []string{root}, Readonly: true,
	}
}
