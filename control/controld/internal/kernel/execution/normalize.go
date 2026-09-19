package executionkernel

import (
	"fmt"
	"mime"
	"path"
	"sort"
	"strings"

	networkpolicy "github.com/cofy-x/axern/lib/go/networkpolicy"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const MaxDeclaredOutputs = 16

func NormalizeConfig(in *commonv1.ExecutionConfig) *commonv1.ExecutionConfig {
	out := &commonv1.ExecutionConfig{}
	if in != nil {
		out = proto.Clone(in).(*commonv1.ExecutionConfig)
	}
	out.Resources = NormalizeResources(out.GetResources())
	out.SecretEnv = normalizeSecretEnv(out.GetSecretEnv())
	out.SecretFiles = normalizeSecretFiles(out.GetSecretFiles())
	out.ImageMounts = NormalizeImageMounts(out.GetImageMounts())
	out.DeclaredOutputs = normalizeDeclaredOutputs(out.GetDeclaredOutputs())
	if network, err := networkpolicy.Normalize(out.GetNetwork()); err == nil {
		out.Network = network
	}
	out.ExtensionCapabilityRequirements = normalizeExtensionCapabilityRequirements(out.GetExtensionCapabilityRequirements())
	return out
}

func normalizeSecretEnv(in []*commonv1.SecretEnvVar) []*commonv1.SecretEnvVar {
	if len(in) == 0 {
		return nil
	}
	out := make([]*commonv1.SecretEnvVar, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		out = append(out, &commonv1.SecretEnvVar{
			Name:     strings.TrimSpace(item.GetName()),
			SecretID: strings.TrimSpace(item.GetSecretID()),
			Key:      strings.TrimSpace(item.GetKey()),
			Optional: item.GetOptional(),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeSecretFiles(in []*commonv1.SecretFile) []*commonv1.SecretFile {
	if len(in) == 0 {
		return nil
	}
	out := make([]*commonv1.SecretFile, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		out = append(out, &commonv1.SecretFile{
			Path:     path.Clean(strings.TrimSpace(item.GetPath())),
			SecretID: strings.TrimSpace(item.GetSecretID()),
			Key:      strings.TrimSpace(item.GetKey()),
			Mode:     item.GetMode(),
			Optional: item.GetOptional(),
		})
	}
	if len(out) == 0 {
		return nil
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
			Image:  image,
			Target: target,
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
	if readonly && in.GetRootfsSnapshot() != nil {
		return nil, grpcstatus.Error(codes.InvalidArgument, "config.rootfs_snapshot requires a writable rootfs")
	}
	if err := validateResourceSigns(in.GetResources()); err != nil {
		return nil, err
	}
	if err := validateExtensionCapabilityRequirements(in.GetExtensionCapabilityRequirements()); err != nil {
		return nil, err
	}
	if err := ValidateNetwork(in.GetNetwork()); err != nil {
		return nil, err
	}
	if err := validateDeclaredOutputs(in.GetDeclaredOutputs()); err != nil {
		return nil, err
	}
	out := NormalizeConfig(in)
	resources, err := NormalizeResourcesForRootfs(out.GetResources(), readonly)
	if err != nil {
		return nil, err
	}
	out.Resources = resources
	return out, ValidateResources(out.Resources)
}

func normalizeDeclaredOutputs(in []*commonv1.DeclaredOutput) []*commonv1.DeclaredOutput {
	out := make([]*commonv1.DeclaredOutput, 0, len(in))
	for _, declared := range in {
		if declared == nil {
			continue
		}
		out = append(out, &commonv1.DeclaredOutput{
			Path:      path.Clean(strings.TrimSpace(declared.GetPath())),
			Format:    declared.GetFormat(),
			MediaType: strings.TrimSpace(declared.GetMediaType()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetPath() < out[j].GetPath() })
	return out
}

func validateDeclaredOutputs(in []*commonv1.DeclaredOutput) error {
	if len(in) > MaxDeclaredOutputs {
		return grpcstatus.Errorf(codes.InvalidArgument, "config.declared_outputs has %d entries; maximum is %d", len(in), MaxDeclaredOutputs)
	}
	seen := make(map[string]struct{}, len(in))
	for _, declared := range in {
		if declared == nil {
			return grpcstatus.Error(codes.InvalidArgument, "config.declared_outputs entry is required")
		}
		rawPath := strings.TrimSpace(declared.GetPath())
		cleanPath := path.Clean(rawPath)
		if rawPath == "" || cleanPath == "/" || !strings.HasPrefix(cleanPath, "/") || hasParentPathElement(rawPath) {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.declared_outputs path %q must be an absolute sandbox path below /", rawPath)
		}
		if _, ok := seen[cleanPath]; ok {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.declared_outputs path %q is duplicated", cleanPath)
		}
		seen[cleanPath] = struct{}{}
		switch declared.GetFormat() {
		case commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE,
			commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR:
		default:
			return grpcstatus.Errorf(codes.InvalidArgument, "config.declared_outputs path %q has unsupported format %s", cleanPath, declared.GetFormat())
		}
		mediaType := strings.TrimSpace(declared.GetMediaType())
		if len(mediaType) > 128 {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.declared_outputs path %q media_type exceeds 128 bytes", cleanPath)
		}
		if mediaType != "" {
			if _, _, err := mime.ParseMediaType(mediaType); err != nil {
				return grpcstatus.Errorf(codes.InvalidArgument, "config.declared_outputs path %q media_type is invalid", cleanPath)
			}
		}
	}
	return nil
}

func hasParentPathElement(value string) bool {
	for _, element := range strings.Split(value, "/") {
		if element == ".." {
			return true
		}
	}
	return false
}

func ValidateNetwork(in *commonv1.NetworkSpec) error {
	if err := networkpolicy.Validate(in); err != nil {
		return grpcstatus.Errorf(codes.InvalidArgument, "config.network: %v", err)
	}
	return nil
}

func normalizeExtensionCapabilityRequirements(in []*capabilityv1.ExtensionCapabilityRequirement) []*capabilityv1.ExtensionCapabilityRequirement {
	out := make([]*capabilityv1.ExtensionCapabilityRequirement, 0, len(in))
	for _, requirement := range in {
		if requirement == nil || requirement.GetCapability() == nil {
			out = append(out, requirement)
			continue
		}
		out = append(out, &capabilityv1.ExtensionCapabilityRequirement{Capability: capabilitycontract.NormalizeExtension(requirement.GetCapability())})
	}
	return out
}

func validateExtensionCapabilityRequirements(in []*capabilityv1.ExtensionCapabilityRequirement) error {
	if err := capabilitycontract.ValidateExtensionRequirements(in); err != nil {
		return grpcstatus.Errorf(codes.InvalidArgument, "config.extension_capability_requirements: %v", err)
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
