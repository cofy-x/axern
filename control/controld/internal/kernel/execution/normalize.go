package executionkernel

import (
	"fmt"
	"path"
	"strings"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func NormalizeConfig(in *commonv1.ExecutionConfig) *commonv1.ExecutionConfig {
	out := &commonv1.ExecutionConfig{}
	if in != nil {
		out = proto.Clone(in).(*commonv1.ExecutionConfig)
	}
	out.Resources = NormalizeResources(out.GetResources())
	out.ImageMounts = NormalizeImageMounts(out.GetImageMounts())
	out.WorkspaceImage = NormalizeWorkspaceImage(out.GetWorkspaceImage())
	out.ExtensionCapabilityRequirements = normalizeExtensionCapabilityRequirements(out.GetExtensionCapabilityRequirements())
	return out
}

func NormalizeWorkspaceImage(in *commonv1.WorkspaceImageSource) *commonv1.WorkspaceImageSource {
	if in == nil {
		return nil
	}
	out := &commonv1.WorkspaceImageSource{SourcePath: strings.TrimSpace(in.GetSourcePath()), Target: path.Clean(strings.TrimSpace(in.GetTarget()))}
	if out.Target == "." {
		out.Target = "/workspace"
	}
	for _, variant := range in.GetVariants() {
		if variant == nil {
			continue
		}
		out.Variants = append(out.Variants, &commonv1.WorkspaceImageVariant{Format: strings.ToLower(strings.TrimSpace(variant.GetFormat())), Image: strings.TrimSpace(variant.GetImage())})
	}
	return out
}

func NormalizeImageMounts(in []*commonv1.ImageMount) []*commonv1.ImageMount {
	if len(in) == 0 {
		return nil
	}
	out := make([]*commonv1.ImageMount, 0, len(in))
	for _, mount := range in {
		if mount == nil {
			continue
		}
		image := strings.TrimSpace(mount.GetImage())
		target := path.Clean(strings.TrimSpace(mount.GetTarget()))
		out = append(out, &commonv1.ImageMount{
			Image:    image,
			Target:   target,
			Readonly: true,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func NormalizeResources(in *commonv1.ResourceSpec) *commonv1.ResourceSpec {
	out := &commonv1.ResourceSpec{}
	if in != nil {
		out = proto.Clone(in).(*commonv1.ResourceSpec)
	}
	if out.Requests == nil {
		out.Requests = &commonv1.ResourceQuantity{}
	}
	limits := out.GetLimits()
	if out.Requests.CpuMilli <= 0 {
		out.Requests.CpuMilli = defaultRequest(limits.GetCpuMilli(), DefaultCPUMilli)
	}
	if out.Requests.MemoryBytes <= 0 {
		out.Requests.MemoryBytes = defaultRequest(limits.GetMemoryBytes(), DefaultMemoryBytes)
	}
	if out.Requests.EphemeralStorageBytes <= 0 && limits.GetEphemeralStorageBytes() > 0 {
		out.Requests.EphemeralStorageBytes = limits.GetEphemeralStorageBytes()
	}
	return out
}

// NormalizeResourcesForRootfs resolves the ephemeral-storage contract once the
// selected environment's rootfs readonly property is known.
func NormalizeResourcesForRootfs(in *commonv1.ResourceSpec, readonly bool) (*commonv1.ResourceSpec, error) {
	out := NormalizeResources(in)
	requested := out.GetRequests().GetEphemeralStorageBytes()
	limit := out.GetLimits().GetEphemeralStorageBytes()
	if readonly {
		if requested != 0 || limit != 0 {
			return nil, grpcstatus.Errorf(codes.InvalidArgument, "readonly rootfs conflicts with ephemeral storage resources: request=%d limit=%d", requested, limit)
		}
		return out, nil
	}
	if out.Limits == nil {
		out.Limits = &commonv1.ResourceQuantity{}
	}
	if limit == 0 {
		limit = DefaultEphemeralStorageBytes
		out.Limits.EphemeralStorageBytes = limit
	}
	if requested == 0 {
		out.Requests.EphemeralStorageBytes = limit
	}
	return out, nil
}

func NormalizeConfigForRootfs(in *commonv1.ExecutionConfig, readonly bool) (*commonv1.ExecutionConfig, error) {
	if err := validateResourceSigns(in.GetResources()); err != nil {
		return nil, err
	}
	out := NormalizeConfig(in)
	if err := validateExtensionCapabilityRequirements(out.GetExtensionCapabilityRequirements()); err != nil {
		return nil, err
	}
	resources, err := NormalizeResourcesForRootfs(out.GetResources(), readonly)
	if err != nil {
		return nil, err
	}
	out.Resources = resources
	return out, ValidateResources(out.Resources)
}

func normalizeExtensionCapabilityRequirements(in []*capabilityv1.ExtensionCapabilityRequirement) []*capabilityv1.ExtensionCapabilityRequirement {
	out := make([]*capabilityv1.ExtensionCapabilityRequirement, 0, len(in))
	for _, requirement := range in {
		if requirement == nil || requirement.GetCapability() == nil {
			continue
		}
		out = append(out, &capabilityv1.ExtensionCapabilityRequirement{Capability: capabilitycontract.NormalizeExtension(requirement.GetCapability())})
	}
	return out
}

func validateExtensionCapabilityRequirements(in []*capabilityv1.ExtensionCapabilityRequirement) error {
	seen := make(map[string]struct{}, len(in))
	for _, requirement := range in {
		if err := capabilitycontract.ValidateExtensionRequirement(requirement); err != nil {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.extension_capability_requirements: %v", err)
		}
		id, _ := capabilitycontract.KeyID(capabilitycontract.ExtensionKey(requirement.GetCapability().GetName(), requirement.GetCapability().GetValue()))
		if _, duplicate := seen[id]; duplicate {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.extension_capability_requirements contains duplicate %q", requirement.GetCapability().GetName())
		}
		seen[id] = struct{}{}
	}
	return nil
}

func defaultRequest(limit, fallback int64) int64 {
	if limit > 0 {
		return limit
	}
	return fallback
}

func ValidateResources(in *commonv1.ResourceSpec) error {
	if err := validateResourceSigns(in); err != nil {
		return err
	}

	normalized := NormalizeResources(in)
	requests := normalized.GetRequests()
	limits := normalized.GetLimits()
	if limits.GetCpuMilli() > 0 && requests.GetCpuMilli() > limits.GetCpuMilli() {
		return grpcstatus.Error(codes.InvalidArgument, fmt.Sprintf("config.resources.requests.cpu_milli must be <= limits.cpu_milli: request=%d limit=%d", requests.GetCpuMilli(), limits.GetCpuMilli()))
	}
	if limits.GetMemoryBytes() > 0 && requests.GetMemoryBytes() > limits.GetMemoryBytes() {
		return grpcstatus.Error(codes.InvalidArgument, fmt.Sprintf("config.resources.requests.memory_bytes must be <= limits.memory_bytes: request=%d limit=%d", requests.GetMemoryBytes(), limits.GetMemoryBytes()))
	}
	if limits.GetEphemeralStorageBytes() > 0 && requests.GetEphemeralStorageBytes() > limits.GetEphemeralStorageBytes() {
		return grpcstatus.Error(codes.InvalidArgument, fmt.Sprintf("config.resources.requests.ephemeral_storage_bytes must be <= limits.ephemeral_storage_bytes: request=%d limit=%d", requests.GetEphemeralStorageBytes(), limits.GetEphemeralStorageBytes()))
	}
	return nil
}

func validateResourceSigns(in *commonv1.ResourceSpec) error {
	if in.GetRequests().GetCpuMilli() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.requests.cpu_milli must be >= 0")
	}
	if in.GetRequests().GetMemoryBytes() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.requests.memory_bytes must be >= 0")
	}
	if in.GetRequests().GetEphemeralStorageBytes() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.requests.ephemeral_storage_bytes must be >= 0")
	}
	if in.GetLimits().GetCpuMilli() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.limits.cpu_milli must be >= 0")
	}
	if in.GetLimits().GetMemoryBytes() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.limits.memory_bytes must be >= 0")
	}
	if in.GetLimits().GetEphemeralStorageBytes() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.limits.ephemeral_storage_bytes must be >= 0")
	}

	return nil
}
