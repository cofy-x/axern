package container

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/jsonutil"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir()
}

func TestCleanContainerRoot(t *testing.T) {
	root := t.TempDir()
	id := "test-container-id"

	m := &Manager{
		root: root,
	}

	rootPath := filepath.Join(root, id)
	err := os.Mkdir(rootPath, 0755)
	assert.NoError(t, err)

	require.NoError(t, m.CleanContainerRoot(id))
	assert.False(t, dirExists(rootPath))
}

type releaseTrackingResourceManager struct {
	name               resourcemanager.ResourceName
	recycled           []string
	using              []string
	recycleAttempts    int
	failuresBeforePass int
}

func (m *releaseTrackingResourceManager) Allocate(resourcemanager.AllocateOption) (resourcemanager.Resource, error) {
	return resourcemanager.EmptyStringResource, errord.ErrResourceExhausted
}

func (m *releaseTrackingResourceManager) Recycle(id string) error {
	m.recycleAttempts++
	if m.failuresBeforePass > 0 {
		m.failuresBeforePass--
		return errors.New("recycle failed")
	}
	m.recycled = append(m.recycled, id)
	return nil
}

func (m *releaseTrackingResourceManager) Status() ([]string, []string) { return m.using, nil }

func (m *releaseTrackingResourceManager) ShutDown() error { return nil }

func (m *releaseTrackingResourceManager) ResourceName() resourcemanager.ResourceName {
	return m.name
}

func TestOccupyPreservesResourceExhaustedClass(t *testing.T) {
	resourceManager := &releaseTrackingResourceManager{name: resourcemanager.InterfaceResourceName}
	manager := &Manager{
		containers:       cmap.New[*Container](),
		resourceManagers: cmap.New[resourcemanager.Manager](),
	}
	manager.resourceManagers.Set(string(resourceManager.ResourceName()), resourceManager)

	_, err := manager.Occupy(
		resourcemanager.AllocateOption{ContainerID: "alloc-test"},
		resourcemanager.InterfaceResourceName,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, errord.ErrResourceExhausted)
}

func TestReleaseResourceKeepsCgroupAssignedUntilOtherResourcesRetire(t *testing.T) {
	cgroupManager := &releaseTrackingResourceManager{name: resourcemanager.CgroupResourceName}
	interfaceManager := &releaseTrackingResourceManager{
		name: resourcemanager.InterfaceResourceName, failuresBeforePass: 1,
	}
	m := &Manager{resourceManagers: cmap.New[resourcemanager.Manager]()}
	m.resourceManagers.Set(string(cgroupManager.ResourceName()), cgroupManager)
	m.resourceManagers.Set(string(interfaceManager.ResourceName()), interfaceManager)
	resources := map[resourcemanager.ResourceName]string{
		resourcemanager.CgroupResourceName:    "/sandbox/test",
		resourcemanager.InterfaceResourceName: "net-resource",
	}

	require.Error(t, m.ReleaseResource(resources))
	assert.Empty(t, cgroupManager.recycled, "cgroup commitment must survive incomplete prerequisite cleanup")
	assert.Equal(t, 1, interfaceManager.recycleAttempts)

	require.NoError(t, m.ReleaseResource(resources))
	assert.Equal(t, []string{"net-resource"}, interfaceManager.recycled)
	assert.Equal(t, []string{"/sandbox/test"}, cgroupManager.recycled)
}

func TestDeletePreservesContainerClaimsUntilResourceReleaseSucceeds(t *testing.T) {
	root := t.TempDir()
	resourceManager := &releaseTrackingResourceManager{
		name:               resourcemanager.InterfaceResourceName,
		failuresBeforePass: 1,
	}
	containerID := "alloc-retry-release"
	m := &Manager{
		root:             root,
		containers:       cmap.New[*Container](),
		resourceManagers: cmap.New[resourcemanager.Manager](),
		idGenerator:      truncindex.NewTruncGenerator("sandbox", []string{containerID}),
		monitors:         cmap.New[*containerMonitor](),
	}
	m.resourceManagers.Set(string(resourceManager.ResourceName()), resourceManager)

	containerDir := filepath.Join(root, containerID)
	require.NoError(t, os.MkdirAll(containerDir, 0755))
	spec := &specs.Spec{
		Annotations: map[string]string{
			resourcemanager.ResourceAnnotationKeyPrefix + string(resourcemanager.InterfaceResourceName): "net-resource",
		},
		Linux: &specs.Linux{},
	}
	buf, err := jsonutil.UnescapedMarshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(containerDir, config.ContainerSpecFile), buf, 0644))
	m.containers.Set(containerID, &Container{
		Metadata: &apipb.ContainerMetadata{ID: containerID, RuntimeHandler: "runsc"},
		Status: &statusStorage{status: Status{
			FinishedAt:    time.Now().UTC().Format(time.RFC3339Nano),
			ExitCodeKnown: true,
		}},
		Spec: spec,
		PATH: containerDir,
	})

	err = m.DeleteAfterConfirmedRuntimeDelete(containerID, OccupiedResource{
		ID: containerID,
		Resources: map[resourcemanager.ResourceName]string{
			resourcemanager.InterfaceResourceName: "net-resource",
		},
	})
	require.ErrorContains(t, err, "recycle failed")
	assert.True(t, dirExists(containerDir))
	assert.True(t, m.containers.Has(containerID))
	assert.Contains(t, spec.Annotations, resourcemanager.ResourceAnnotationKeyPrefix+string(resourcemanager.InterfaceResourceName))

	require.NoError(t, m.DeleteAfterConfirmedRuntimeDelete(containerID, OccupiedResource{
		ID: containerID,
		Resources: map[resourcemanager.ResourceName]string{
			resourcemanager.InterfaceResourceName: "net-resource",
		},
	}))
	assert.False(t, dirExists(containerDir))
	assert.False(t, m.containers.Has(containerID))
	assert.Equal(t, 2, resourceManager.recycleAttempts)
	assert.Equal(t, []string{"net-resource"}, resourceManager.recycled)
}

func TestReconcileResourceClaimsRecyclesOnlyUnclaimedPoolOwnership(t *testing.T) {
	resourceManager := &releaseTrackingResourceManager{
		name:  resourcemanager.InterfaceResourceName,
		using: []string{"owned-resource", "orphan-resource"},
	}
	m := &Manager{
		containers:       cmap.New[*Container](),
		resourceManagers: cmap.New[resourcemanager.Manager](),
	}
	m.resourceManagers.Set(string(resourceManager.ResourceName()), resourceManager)
	m.containers.Set("alloc-owned", &Container{Spec: &specs.Spec{
		Version: "1.0.0",
		Annotations: map[string]string{
			resourcemanager.ResourceAnnotationKeyPrefix + string(resourcemanager.InterfaceResourceName): "owned-resource",
		},
	}})

	require.NoError(t, m.ReconcileResourceClaims())
	assert.Equal(t, []string{"orphan-resource"}, resourceManager.recycled)
}

func TestReconcileResourceClaimsRejectsContainerWithoutRecoverableSpec(t *testing.T) {
	m := &Manager{
		containers:       cmap.New[*Container](),
		resourceManagers: cmap.New[resourcemanager.Manager](),
	}
	m.containers.Set("alloc-corrupt", &Container{Spec: &specs.Spec{}})

	require.ErrorContains(t, m.ReconcileResourceClaims(), "no recoverable OCI spec")
}
