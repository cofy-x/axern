package container

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/stretchr/testify/assert"
)

func newTestContainers(hitId string) []*Container {
	var containers []*Container

	containers = append(containers, nil)
	containers = append(containers, &Container{})
	containers = append(containers, &Container{
		ID:       "test-1",
		Metadata: &apipb.ContainerMetadata{},
	})

	if len(hitId) != 0 {
		containers = append(containers, &Container{
			ID:       hitId,
			Metadata: &apipb.ContainerMetadata{},
		})
	}

	return containers
}

func callFilter(containers []*Container, opt ListOption) []*Container {
	var hitContainers []*Container
	for _, c := range containers {
		if opt(c) {
			hitContainers = append(hitContainers, c)
		}
	}
	return hitContainers
}

func TestListFilterById(t *testing.T) {
	hitId := "hitid"
	containers := newTestContainers(hitId)

	hitContainers := callFilter(containers, ListFilterById(hitId))

	assert.Equal(t, 1, len(hitContainers))
	assert.Equal(t, hitContainers[0].ID, hitId)
}
