package nodebridge

import (
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	privatenodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"github.com/cofy-x/axern/lib/go/imageref"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/protobuf/proto"
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
		AllocationID:             params.AllocationID,
		NodeID:                   params.NodeID,
		Config:                   buildResolvedExecutionConfig(params),
		ExecutionLeaseTtlSeconds: int64(allocationkernel.ExecutionLeaseTTL / time.Second),
	}
}

type resolvedExecutionSecrets struct {
	EnvSecrets       []*privatenodev1.ResolvedSecretEnvVar
	FileSecrets      []*privatenodev1.ResolvedSecretFile
	DockerConfigJSON string
}

func buildResolvedExecutionConfig(params createAllocationRequestParams) *privatenodev1.ResolvedExecutionConfig {
	resolvedSpec := params.Environment.GetResolvedSpec()
	cfg := configOrEmpty(params.Config)
	res := executionkernel.NormalizeResources(cfg.GetResources())

	out := &privatenodev1.ResolvedExecutionConfig{
		EnvironmentID:                   params.Environment.GetID(),
		ImageDigest:                     resolvedSpec.GetImageDescriptor().GetDigest(),
		ImageDescriptor:                 imageDescriptorRef(params.Environment),
		Argv:                            resolveExecutionArgv(cfg.GetArgv()),
		Cwd:                             resolveExecutionCwd(cfg.GetCwd()),
		Env:                             mergeStringMaps(resolvedSpec.GetDefaultEnv(), cfg.GetEnv()),
		Resources:                       res,
		ExtensionCapabilityRequirements: cloneExtensionCapabilityRequirements(cfg.GetExtensionCapabilityRequirements()),
		LocalityKey:                     firstNonEmpty(params.Environment.GetID(), resolvedSpec.GetImageDescriptor().GetDigest()),
		RootfsReadonly:                  resolvedSpec.GetRootfsReadonly(),
		Network:                         cloneNetworkSpec(cfg.GetNetwork()),
		SecretEnv:                       cloneResolvedSecretEnvVars(params.ResolvedSecrets.EnvSecrets),
		SecretFiles:                     cloneResolvedSecretFiles(params.ResolvedSecrets.FileSecrets),
		ExecutionProfile:                cloneOciExecutionProfile(resolvedSpec.GetExecutionProfile()),
		ImageMounts:                     cloneImageMounts(cfg.GetImageMounts()),
		CapabilityRequirements:          cloneCapabilityRequirements(params.CapabilityRequirements),
		DeclaredOutputs:                 cloneDeclaredOutputs(cfg.GetDeclaredOutputs()),
		RootfsSnapshot:                  cloneRootfsSnapshot(cfg.GetRootfsSnapshot()),
	}
	if strings.TrimSpace(params.ResolvedSecrets.DockerConfigJSON) != "" {
		out.RegistryCredential = &privatenodev1.RegistryCredential{DockerConfigJson: params.ResolvedSecrets.DockerConfigJSON}
	}
	for _, mount := range resolvedSpec.GetMounts() {
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

func cloneRootfsSnapshot(in *commonv1.RootfsSnapshot) *commonv1.RootfsSnapshot {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*commonv1.RootfsSnapshot)
}

func cloneDeclaredOutputs(in []*commonv1.DeclaredOutput) []*commonv1.DeclaredOutput {
	out := make([]*commonv1.DeclaredOutput, 0, len(in))
	for _, declared := range in {
		if declared != nil {
			out = append(out, proto.Clone(declared).(*commonv1.DeclaredOutput))
		}
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

func imageDescriptorRef(environment *environmentv1.Environment) string {
	desc := environment.GetResolvedSpec().GetImageDescriptor()
	if desc == nil {
		return ""
	}
	// Image-backed Environments retain the caller-visible repository in their
	// immutable specification and the resolved content identity in the OCI
	// descriptor. Bind those two facts here before crossing the node boundary;
	// a bare digest has no registry/repository context and must never be guessed
	// by the node.
	if ref := strings.TrimSpace(environment.GetSpec().GetImage().GetRef()); ref != "" {
		if immutable, err := imageref.WithDigest(ref, desc.GetDigest()); err == nil {
			return immutable
		}
	}
	// Template-backed Environments already carry the deployment-owned runtime
	// reference in the resolved descriptor. Keep that reference intact: source
	// and single-node deployments may intentionally bind it to a node-local
	// imported image, while release deployments own their registry publication
	// policy. Combining an override tag with the embedded catalog digest
	// manufactures an identity that neither the node import nor a registry owns.
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
