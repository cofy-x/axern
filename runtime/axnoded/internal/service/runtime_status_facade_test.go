package service

import (
	"context"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/assert"
)

func TestVersion(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})

	resp, err := s.Version(context.Background(), &runtime.VersionRequest{Version: "0.0.1"})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Version)
	assert.Len(t, resp.Runtimes, 1)
}

func TestVersion_NoRuntimes(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{})

	resp, err := s.Version(context.Background(), &runtime.VersionRequest{Version: "0.0.1"})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Version)
	assert.Empty(t, resp.Runtimes)
}

func TestRuntimeStatuses(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": &runtimeSpyHandler{
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
	})
	s.config.PluginConfig.RuntimeConfig.Runtimes["runc"] = config.RuntimeInstanceConfig{
		Binary: "/fake/runc",
	}

	statuses := s.RuntimeStatuses()
	assert.Len(t, statuses, 2)
	assert.Equal(t, "runc", statuses[0].Name)
	assert.False(t, statuses[0].Loaded)
	assert.Equal(t, "/fake/runc", statuses[0].Binary)
	assert.Equal(t, "runsc", statuses[1].Name)
	assert.True(t, statuses[1].Loaded)
	assert.Equal(t, []resourcemanager.ResourceName{resourcemanager.CgroupResourceName}, statuses[1].Requirements.Resources)
	assert.True(t, statuses[1].Capabilities.CanCheckpoint)
}

func TestVersion_MultipleRuntimes(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc":  runtimetest.NewFakeRuntimeHandler(),
		"runsc2": runtimetest.NewFakeRuntimeHandler(),
	})

	resp, err := s.Version(context.Background(), &runtime.VersionRequest{})
	assert.NoError(t, err)
	assert.Len(t, resp.Runtimes, 2)
}

func TestCheckRuntime(t *testing.T) {
	tests := []struct {
		name           string
		requestRuntime string
		options        []runtimeStatusFacadeServiceOption
		wantErr        bool
	}{
		{
			name:           "runtime not configured",
			requestRuntime: "nonexistent",
			options: []runtimeStatusFacadeServiceOption{
				setRuntimeConfig(config.RuntimeConfig{
					RuntimeBinary: map[string]string{
						"runsc": "/usr/local/bin/runsc",
					},
				}),
			},
			wantErr: true,
		},
		{
			name:           "runtime handler missing",
			requestRuntime: "runsc",
			options: []runtimeStatusFacadeServiceOption{
				setRuntimeConfig(config.RuntimeConfig{
					RuntimeBinary: map[string]string{
						"runsc": "/usr/local/bin/runsc",
					},
				}),
			},
			wantErr: true,
		},
		{
			name:           "runtime handler configured",
			requestRuntime: "runsc",
			options: []runtimeStatusFacadeServiceOption{
				setRuntimeConfig(config.RuntimeConfig{
					RuntimeBinary: map[string]string{
						"runsc": "/usr/local/bin/runsc",
					},
				}),
				addRuntimeHandler("runsc", runtimetest.NewFakeRuntimeHandler()),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := buildRuntimeStatusFacadeService(tt.options...)
			err := s.checkRuntime(tt.requestRuntime)
			assert.Equal(t, tt.wantErr, err != nil)
		})
	}
}

type runtimeStatusFacadeServiceOption func(*sandboxService)

func buildRuntimeStatusFacadeService(options ...runtimeStatusFacadeServiceOption) *sandboxService {
	s := &sandboxService{
		config:          config.Config{},
		runtimeHandlers: handlerregistry.New(config.Config{}),
	}
	for _, option := range options {
		option(s)
	}
	return s
}

func setRuntimeConfig(runtimeConfig config.RuntimeConfig) runtimeStatusFacadeServiceOption {
	return func(service *sandboxService) {
		service.config.PluginConfig.RuntimeConfig = runtimeConfig
	}
}

func addRuntimeHandler(runtimeName string, handler contract.RuntimeHandler) runtimeStatusFacadeServiceOption {
	return func(service *sandboxService) {
		service.runtimeHandlers.Set(runtimeName, handler)
	}
}
