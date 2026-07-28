package container

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/stretchr/testify/assert"
)

func newTestContainers(hitId string, hitLabel map[string]string) []*Container {
	var containers []*Container

	containers = append(containers, nil)
	containers = append(containers, &Container{})
	containers = append(containers, &Container{
		Metadata: &apipb.ContainerMetadata{ID: "test-1"},
	})

	if len(hitId) != 0 {
		containers = append(containers, &Container{
			Metadata: &apipb.ContainerMetadata{ID: hitId},
		})
	}

	containers = append(containers, &Container{
		Metadata: &apipb.ContainerMetadata{ID: "label-not-hit", Labels: hitLabel},
	})

	containers = append(containers, &Container{
		Metadata: &apipb.ContainerMetadata{ID: "label-hit", Labels: map[string]string{
			"test-999": "666",
		}},
	})

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
	containers := newTestContainers(hitId, nil)

	hitContainers := callFilter(containers, ListFilterById(hitId))

	assert.Equal(t, 1, len(hitContainers))
	assert.Equal(t, hitContainers[0].Metadata.ID, hitId)
}

func TestListFilterByLabels(t *testing.T) {
	hitLabel := map[string]string{
		"hitKey": "hitValue",
	}
	containers := newTestContainers("", hitLabel)

	hitContainers := callFilter(containers, ListFilterByLabels(hitLabel))

	assert.Equal(t, 1, len(hitContainers))
	assert.Equal(t, hitContainers[0].Metadata.Labels["hitKey"], "hitValue")
}
