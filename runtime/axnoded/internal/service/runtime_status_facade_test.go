package service

import (
	"context"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/assert"
)

func TestVersion(t *testing.T) {
	s := newTestService(t,
		runtimetest.NewFakeRuntimeHandler(),
	)

	resp, err := s.Version(context.Background(), &runtime.VersionRequest{Version: "0.0.1"})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Version)
	assert.NotNil(t, resp.Runsc)
}

func TestVersion_NoRunsc(t *testing.T) {
	s := newTestService(t, nil)

	resp, err := s.Version(context.Background(), &runtime.VersionRequest{Version: "0.0.1"})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Version)
	assert.Nil(t, resp.Runsc)
}

func TestRuntimeStatuses(t *testing.T) {
	s := newTestService(t,
		&runtimeSpyHandler{
			name: "runsc",
			capabilities: contract.RuntimeCapabilities{
				CanCheckpoint: true,
			},
			requirements: contract.RuntimeRequirements{
				Resources: []resourcemanager.ResourceName{
					resourcemanager.CgroupResourceName,
				},
			},
		},
	)
	statuses := s.RuntimeStatuses()
	assert.Len(t, statuses, 1)
	assert.Equal(t, "runsc", statuses[0].Name)
	assert.True(t, statuses[0].Loaded)
	assert.Equal(t, []resourcemanager.ResourceName{resourcemanager.CgroupResourceName}, statuses[0].Requirements.Resources)
	assert.True(t, statuses[0].Capabilities.CanCheckpoint)
}

func TestVersionReportsRunsc(t *testing.T) {
	s := newTestService(t,
		runtimetest.NewFakeRuntimeHandler(),
	)

	resp, err := s.Version(context.Background(), &runtime.VersionRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp.Runsc)
}
