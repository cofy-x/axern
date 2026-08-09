package startplan

import (
	"encoding/json"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/probes"
	"github.com/sirupsen/logrus"
)

type ExtraConfig struct {
	BlockNetwork      bool                 `json:"blockNetwork,omitempty"`
	CIDRAllowlist     string               `json:"cidrAllowlist,omitempty"`
	LinuxCapabilities []string             `json:"linuxCapabilities,omitempty"`
	DockerConfigJSON  string               `json:"dockerConfigJson,omitempty"`
	Namespace         string               `json:"namespace,omitempty"`
	ServiceID         string               `json:"serviceId,omitempty"`
	SecretEnv         []ResolvedSecretEnv  `json:"secretEnv,omitempty"`
	SecretFiles       []ResolvedSecretFile `json:"secretFiles,omitempty"`
	ReadinessProbe    *probes.Config       `json:"readinessProbe,omitempty"`
	LivenessProbe     *probes.Config       `json:"livenessProbe,omitempty"`
}

type ResolvedSecretEnv struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

type ResolvedSecretFile struct {
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
	Mode    uint32 `json:"mode,omitempty"`
}

func ParseExtraConfig(raw string) (ExtraConfig, bool) {
	if raw == "" {
		return ExtraConfig{}, false
	}
	var extraConfig ExtraConfig
	if err := json.Unmarshal([]byte(raw), &extraConfig); err != nil {
		logrus.WithError(err).Warn("unmarshal extra config failed")
		return ExtraConfig{}, false
	}
	return extraConfig, true
}

func RootfsConfigFromStartRequest(request *runtime.StartRequest) (langrtmanager.RootfsConfig, error) {
	cfg, err := langrtmanager.RootfsConfigFromRuntimeTemplate(request.GetRuntimeTemplate())
	if err != nil {
		return cfg, err
	}
	extraConfig, ok := ParseExtraConfig(request.GetExtraConfig())
	if ok {
		cfg.DockerConfigJSON = strings.TrimSpace(extraConfig.DockerConfigJSON)
	}
	return cfg, nil
}
