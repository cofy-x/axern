package sandboxcontrol

import (
	"context"
	"testing"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeKillSignal(t *testing.T) {
	assert.Equal(t, "TERM", normalizeKillSignal(""))
	assert.Equal(t, "TERM", normalizeKillSignal("sigterm"))
	assert.Equal(t, "KILL", normalizeKillSignal("SIGKILL"))
	assert.Equal(t, "9", normalizeKillSignal("9"))
}

func TestListFiltersByID(t *testing.T) {
	containers := []*container.Container{
		testContainer("ctr-a"),
		testContainer("ctr-b"),
	}
	controller := NewController(Options{
		ListContainers: func(filters ...container.ListOption) []*container.Container {
			var out []*container.Container
			for _, item := range containers {
				matches := true
				for _, filter := range filters {
					if !filter(item) {
						matches = false
						break
					}
				}
				if matches {
					out = append(out, item)
				}
			}
			return out
		},
	})

	byID, err := controller.List(context.Background(), &runtime.ListContainersRequest{ID: "ctr-a"})
	require.NoError(t, err)
	require.Len(t, byID.GetContainers(), 1)
	assert.Equal(t, "ctr-a", byID.GetContainers()[0].GetID())

	all, err := controller.List(context.Background(), &runtime.ListContainersRequest{})
	require.NoError(t, err)
	require.Len(t, all.GetContainers(), 2)
}

func testContainer(id string) *container.Container {
	return &container.Container{
		ID:       id,
		Metadata: &runtime.ContainerMetadata{},
		Status: fixedStatus{status: container.Status{
			RuntimeState: runtime.RuntimeCheckpointState_RUNTIME_CHECKPOINT_STATE_RUNNING,
			StartedAt:    time.Now().Format(time.RFC3339Nano),
		}},
	}
}

type fixedStatus struct {
	status container.Status
}

func (s fixedStatus) Get() container.Status {
	return s.status
}

func (s fixedStatus) UpdateSync(container.UpdateFunc) error {
	return nil
}

func (s fixedStatus) Update(container.UpdateFunc) error {
	return nil
}

func (s fixedStatus) Delete() error {
	return nil
}
