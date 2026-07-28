package service

import (
	"context"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/assert"
)

func TestList_Empty(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})

	resp, err := s.List(context.Background(), &runtime.ListContainersRequest{})
	assert.NoError(t, err)
	assert.Empty(t, resp.Containers)
}

func TestList_ById_NotFound(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})

	_, err := s.List(context.Background(), &runtime.ListContainersRequest{
		ID: "axctl-nonexistent",
	})
	assert.Error(t, err)
}

func TestList_WithStoredContainer(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})

	containerID := "axctl-test-list-001"
	meta := &apipb.ContainerMetadata{
		ID:             containerID,
		RuntimeHandler: "runsc",
		Labels:         map[string]string{"env": "test"},
		Stdout:         "/tmp/stdout.log",
		Stderr:         "/tmp/stderr.log",
	}

	s.containerManager.StoreMetadata(containerID, meta)
	time.Sleep(200 * time.Millisecond)

	resp, err := s.List(context.Background(), &runtime.ListContainersRequest{})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(resp.Containers), 1)

	found := false
	for _, c := range resp.Containers {
		if c.ID == containerID {
			found = true
			assert.Equal(t, "runsc", c.Runtime)
			break
		}
	}
	assert.True(t, found, "stored container should appear in list")
}

func TestConfigureSandboxControlDefersContainerManagerLookup(t *testing.T) {
	base := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})
	containerID := "axctl-sandbox-control-deferred"
	base.containerManager.StoreMetadata(containerID, &apipb.ContainerMetadata{
		ID:             containerID,
		RuntimeHandler: "runsc",
	})
	time.Sleep(200 * time.Millisecond)

	early := &sandboxService{}
	early.configureSandboxControl()
	early.containerManager = base.containerManager

	resp, err := early.List(context.Background(), &runtime.ListContainersRequest{ID: containerID})
	assert.NoError(t, err)
	if assert.Len(t, resp.GetContainers(), 1) {
		assert.Equal(t, containerID, resp.GetContainers()[0].GetID())
	}
}

func TestList_ByLabel(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})

	containerID := "axctl-test-label-001"
	meta := &apipb.ContainerMetadata{
		ID:             containerID,
		RuntimeHandler: "runsc",
		Labels:         map[string]string{"app": "myapp"},
	}
	s.containerManager.StoreMetadata(containerID, meta)
	time.Sleep(200 * time.Millisecond)

	resp, err := s.List(context.Background(), &runtime.ListContainersRequest{
		Selector: map[string]string{"app": "myapp"},
	})
	assert.NoError(t, err)
	found := false
	for _, c := range resp.Containers {
		if c.ID == containerID {
			found = true
		}
	}
	assert.True(t, found)

	resp, err = s.List(context.Background(), &runtime.ListContainersRequest{
		Selector: map[string]string{"app": "other"},
	})
	assert.NoError(t, err)
	for _, c := range resp.Containers {
		assert.NotEqual(t, containerID, c.ID)
	}
}
