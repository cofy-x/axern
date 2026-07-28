package rollout

import (
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	axernbackend "github.com/cofy-x/axern/apps/axrun/internal/backend/axern"
	localbackend "github.com/cofy-x/axern/apps/axrun/internal/backend/local"
)

type BackendRequest struct {
	BackendName string
	AgentName   string
	AgentImage  string
	Now         func() time.Time
	Registry    *agent.Registry
	AxernConfig *axernbackend.Config
}

func (s Service) newBackend(params Params) (backend.Backend, error) {
	registry := s.registry()
	request := BackendRequest{
		BackendName: params.BackendName,
		AgentName:   params.Agent,
		AgentImage:  params.AgentImage,
		Now:         s.Now,
		Registry:    registry,
		AxernConfig: params.AxernConfig,
	}
	if s.BackendFactory != nil {
		return s.BackendFactory(request)
	}
	return newBackend(request)
}

func (s Service) registry() *agent.Registry {
	if s.AgentRegistry != nil {
		return s.AgentRegistry
	}
	return agentcatalog.DefaultRegistry()
}

func newBackend(request BackendRequest) (backend.Backend, error) {
	switch backend.Name(request.BackendName) {
	case backend.NameLocal:
		return localbackend.Adapter{
			Now:       request.Now,
			AgentName: request.AgentName,
			Registry:  request.Registry,
		}, nil
	case backend.NameAxern:
		config := axernbackend.ConfigFromEnv()
		if paramsConfig := requestConfig(request); paramsConfig != nil {
			config = *paramsConfig
		}
		return axernbackend.New(config,
			axernbackend.WithNow(request.Now),
			axernbackend.WithAgentName(request.AgentName),
			axernbackend.WithRegistry(request.Registry),
		), nil
	default:
		return nil, backend.ValidateName(request.BackendName)
	}
}

func requestConfig(request BackendRequest) *axernbackend.Config {
	return request.AxernConfig
}
