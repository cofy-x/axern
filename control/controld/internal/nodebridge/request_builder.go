package nodebridge

import (
	"strings"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/proto"
)

type createAllocationRequestParams struct {
	AllocationID    string
	Attempt         int64
	Config          *commonv1.ExecutionConfig
	Environment     *environmentv1.Environment
	NodeID          string
	DefaultRuntime  string
	Namespace       string
	ServiceID       string
	ReadinessProbe  *servicev1.ServiceProbe
	LivenessProbe   *servicev1.ServiceProbe
	NodeVolumes     []*privatestoragev1.ResolvedNodeVolume
	ResolvedSecrets resolvedExecutionSecrets
}

func buildCreateAllocationRequestFromParams(params createAllocationRequestParams) *privatenodev1.CreateAllocationRequest {
	return &privatenodev1.CreateAllocationRequest{
		AllocationID: params.AllocationID,
		Attempt:      params.Attempt,
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
		EnvironmentID:          params.Environment.GetID(),
		ImageDigest:            template.GetImageDescriptor().GetDigest(),
		ImageDescriptor:        imageDescriptorRef(template.GetImageDescriptor()),
		RuntimeClass:           firstNonEmpty(cfg.GetRuntimeClass(), params.DefaultRuntime),
		Argv:                   resolveExecutionArgv(cfg.GetArgv()),
		Cwd:                    resolveExecutionCwd(cfg.GetCwd()),
		Env:                    mergeStringMaps(template.GetDefaultEnv(), cfg.GetEnv()),
		Resources:              res,
		CapabilityRequirements: cloneCapabilityRequirements(cfg.GetCapabilityRequirements()),
		LocalityKey:            firstNonEmpty(params.Environment.GetID(), template.GetImageDescriptor().GetDigest()),
		RootfsReadonly:         template.GetRootfsReadonly(),
		Ports:                  clonePortSpecs(cfg.GetPorts()),
		Network:                cloneNetworkSpec(cfg.GetNetwork()),
		SecretEnv:              cloneResolvedSecretEnvVars(params.ResolvedSecrets.EnvSecrets),
		SecretFiles:            cloneResolvedSecretFiles(params.ResolvedSecrets.FileSecrets),
		ReadinessProbe:         cloneResolvedProbe(params.ReadinessProbe),
		LivenessProbe:          cloneResolvedProbe(params.LivenessProbe),
		Namespace:              strings.TrimSpace(params.Namespace),
		ServiceID:              strings.TrimSpace(params.ServiceID),
		ExecutionProfile:       cloneRuntimeExecutionProfile(template.GetExecutionProfile()),
		NodeVolumes:            cloneResolvedNodeVolumes(params.NodeVolumes),
		ImageMounts:            cloneImageMounts(cfg.GetImageMounts()),
		WorkspaceImage:         cloneWorkspaceImage(cfg.GetWorkspaceImage()),
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

func cloneWorkspaceImage(in *commonv1.WorkspaceImageSource) *privatenodev1.WorkspaceImageSource {
	if in == nil {
		return nil
	}
	out := &privatenodev1.WorkspaceImageSource{SourcePath: strings.TrimSpace(in.GetSourcePath()), Target: strings.TrimSpace(in.GetTarget())}
	for _, variant := range in.GetVariants() {
		if variant == nil {
			continue
		}
		out.Variants = append(out.Variants, &privatenodev1.WorkspaceImageVariant{Format: strings.TrimSpace(variant.GetFormat()), Image: strings.TrimSpace(variant.GetImage())})
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

func cloneResolvedNodeVolumes(in []*privatestoragev1.ResolvedNodeVolume) []*privatestoragev1.ResolvedNodeVolume {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatestoragev1.ResolvedNodeVolume, 0, len(in))
	for _, volume := range in {
		if volume == nil {
			continue
		}
		out = append(out, proto.Clone(volume).(*privatestoragev1.ResolvedNodeVolume))
	}
	return out
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

func clonePublishedNodeVolumes(in []*privatestoragev1.PublishedNodeVolume) []*privatestoragev1.PublishedNodeVolume {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatestoragev1.PublishedNodeVolume, 0, len(in))
	for _, volume := range in {
		if volume == nil {
			continue
		}
		out = append(out, proto.Clone(volume).(*privatestoragev1.PublishedNodeVolume))
	}
	return out
}

func cloneVolumeReleaseObservations(in []*privatestoragev1.VolumeReleaseObservation) []*privatestoragev1.VolumeReleaseObservation {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatestoragev1.VolumeReleaseObservation, 0, len(in))
	for _, observation := range in {
		if observation == nil {
			continue
		}
		out = append(out, proto.Clone(observation).(*privatestoragev1.VolumeReleaseObservation))
	}
	return out
}
