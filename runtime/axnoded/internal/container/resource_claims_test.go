package container

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
)

func TestRuntimeCgroupPathPrefersSpecPath(t *testing.T) {
	root := t.TempDir()
	m := &Manager{
		root:        root,
		recyclePath: t.TempDir(),
		containers:  cmap.New[*Container](),
	}

	containerID := "axctl-test"
	containerDir := filepath.Join(root, containerID)
	assert.NoError(t, os.MkdirAll(containerDir, 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(containerDir, config.ContainerSpecFile), []byte(`{"ociVersion":"1.0.0","annotations":{"io.axnoded.resource/cgroup":"/sandbox/test"},"linux":{"cgroupsPath":"/sandbox/test/workload"}}`), 0644))

	cgroupPath, err := m.RuntimeCgroupPath(containerID)
	assert.NoError(t, err)
	assert.Equal(t, "/sandbox/test/workload", cgroupPath)
}

func TestRuntimeCgroupPathFallsBackToResourceAnnotation(t *testing.T) {
	root := t.TempDir()
	m := &Manager{
		root:        root,
		recyclePath: t.TempDir(),
		containers:  cmap.New[*Container](),
	}

	containerID := "axctl-test"
	containerDir := filepath.Join(root, containerID)
	assert.NoError(t, os.MkdirAll(containerDir, 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(containerDir, config.ContainerSpecFile), []byte(`{"ociVersion":"1.0.0","annotations":{"io.axnoded.resource/cgroup":"/sandbox/test"},"linux":{"cgroupsPath":""}}`), 0644))

	cgroupPath, err := m.RuntimeCgroupPath(containerID)
	assert.NoError(t, err)
	assert.Equal(t, "/sandbox/test", cgroupPath)
}

func TestParseProcessCgroupPathPrefersUnifiedPath(t *testing.T) {
	cgroupPath, err := parseProcessCgroupPath("0::/sandbox/test/workload\n")
	assert.NoError(t, err)
	assert.Equal(t, "/sandbox/test/workload", cgroupPath)
}

func TestParseProcessCgroupPathFallsBackToMemoryController(t *testing.T) {
	cgroupPath, err := parseProcessCgroupPath("11:cpu:/sandbox/test\n10:memory:/sandbox/test/workload\n")
	assert.NoError(t, err)
	assert.Equal(t, "/sandbox/test/workload", cgroupPath)
}
