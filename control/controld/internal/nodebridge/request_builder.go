package nodebridge

import (
	"strings"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

type createAllocationRequestParams struct {
	AllocationID           string
	Config                 *commonv1.ExecutionConfig
	Environment            *environmentv1.Environment
	NodeID                 string
	ResolvedSecrets        resolvedExecutionSecrets
	CapabilityRequirements []*capabilityv1.CapabilityRequirement
}

func buildCreateAllocationRequestFromParams(params createAllocationRequestParams) *privatenodev1.CreateAllocationRequest {
	return &privatenodev1.CreateAllocationRequest{
		AllocationID: params.AllocationID,
		NodeID:       params.NodeID,
		Config:       buildResolvedExecutionConfig(params),
	}
}

type resolvedExecutionSecrets struct {
	EnvSecrets       []*privatenodev1.ResolvedSecretEnvVar
	FileSecrets      []*privatenodev1.ResolvedSecretFile
	DockerConfigJSON string
}

func buildResolvedExecutionConfig(params createAllocationRequestParams) *privatenodev1.ResolvedExecutionConfig {
	template := params.Environment.GetResolvedTemplate()
	cfg := configOrEmpty(params.Config)
	res := executionkernel.NormalizeResources(cfg.GetResources())

	out := &privatenodev1.ResolvedExecutionConfig{
		EnvironmentID:                   params.Environment.GetID(),
		ImageDigest:                     template.GetImageDescriptor().GetDigest(),
		ImageDescriptor:                 imageDescriptorRef(template.GetImageDescriptor()),
		Argv:                            resolveExecutionArgv(cfg.GetArgv()),
		Cwd:                             resolveExecutionCwd(cfg.GetCwd()),
		Env:                             mergeStringMaps(template.GetDefaultEnv(), cfg.GetEnv()),
		Resources:                       res,
		ExtensionCapabilityRequirements: cloneExtensionCapabilityRequirements(cfg.GetExtensionCapabilityRequirements()),
		LocalityKey:                     firstNonEmpty(params.Environment.GetID(), template.GetImageDescriptor().GetDigest()),
		RootfsReadonly:                  template.GetRootfsReadonly(),
		Ports:                           clonePortSpecs(cfg.GetPorts()),
		Network:                         cloneNetworkSpec(cfg.GetNetwork()),
		SecretEnv:                       cloneResolvedSecretEnvVars(params.ResolvedSecrets.EnvSecrets),
		SecretFiles:                     cloneResolvedSecretFiles(params.ResolvedSecrets.FileSecrets),
		ExecutionProfile:                cloneOciExecutionProfile(template.GetExecutionProfile()),
		ImageMounts:                     cloneImageMounts(cfg.GetImageMounts()),
		CapabilityRequirements:          cloneCapabilityRequirements(params.CapabilityRequirements),
	}
	if strings.TrimSpace(params.ResolvedSecrets.DockerConfigJSON) != "" {
		out.RegistryCredential = &privatenodev1.RegistryCredential{DockerConfigJson: params.ResolvedSecrets.DockerConfigJSON}
	}
	for _, mount := range template.GetMounts() {
		if mount == nil {
			continue
		}
		out.Mounts = append(out.Mounts, &privatenodev1.SandboxMount{
			Type:    mount.GetType(),
			Source:  mount.GetSource(),
			Target:  mount.GetTarget(),
			Options: cloneStringSlice(mount.GetOptions()),
		})
	}
	return out
}

func resolveExecutionArgv(configArgv []string) []string {
	if len(configArgv) > 0 {
		return append([]string(nil), configArgv...)
	}
	return nil
}

func resolveExecutionCwd(configCwd string) string {
	return strings.TrimSpace(configCwd)
}

func configOrEmpty(config *commonv1.ExecutionConfig) *commonv1.ExecutionConfig {
	if config == nil {
		return &commonv1.ExecutionConfig{}
	}
	return config
}

func imageDescriptorRef(desc *catalogv1.OciImageDescriptor) string {
	if desc == nil {
		return ""
	}
	for _, key := range []string{"org.opencontainers.image.ref.name", "io.axern.image.ref"} {
		if ref := strings.TrimSpace(desc.GetAnnotations()[key]); ref != "" {
			return ref
		}
	}
	return strings.TrimSpace(desc.GetDigest())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneImageMounts(in []*commonv1.ImageMount) []*privatenodev1.ImageMount {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatenodev1.ImageMount, 0, len(in))
	for _, mount := range in {
		if mount == nil {
			continue
		}
		out = append(out, &privatenodev1.ImageMount{
			Image:    strings.TrimSpace(mount.GetImage()),
			Target:   strings.TrimSpace(mount.GetTarget()),
			Readonly: true,
		})
	}
	return out
}
