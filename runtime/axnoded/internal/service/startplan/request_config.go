package startplan

import (
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
)

func RootfsConfigFromStartRequest(request *runtime.StartRequest) (environmentcache.RootfsConfig, error) {
	cfg, err := environmentcache.RootfsConfigFromEnvironmentTemplate(request.GetEnvironmentTemplate())
	if err != nil {
		return cfg, err
	}
	cfg.DockerConfigJSON = strings.TrimSpace(request.GetRegistryCredential().GetDockerConfigJson())
	return cfg, nil
}
