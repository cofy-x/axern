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

func TestListFiltersByIDAndLabels(t *testing.T) {
	containers := []*container.Container{
		testContainer("ctr-a", map[string]string{"app": "api"}),
		testContainer("ctr-b", map[string]string{"app": "worker"}),
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

	byLabel, err := controller.List(context.Background(), &runtime.ListContainersRequest{Selector: map[string]string{"app": "worker"}})
	require.NoError(t, err)
	require.Len(t, byLabel.GetContainers(), 1)
	assert.Equal(t, "ctr-b", byLabel.GetContainers()[0].GetID())
}

func testContainer(id string, labels map[string]string) *container.Container {
	return &container.Container{
		Metadata: &runtime.ContainerMetadata{
			ID:             id,
			RuntimeHandler: "runsc",
			Labels:         labels,
		},
		Status: fixedStatus{status: container.Status{
			StartedAt:     time.Now().Format(time.RFC3339Nano),
			ExitCodeKnown: true,
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
