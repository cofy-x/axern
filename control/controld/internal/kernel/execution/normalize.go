package executionkernel

import (
	"fmt"
	"path"
	"strings"

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
	return out
}

func defaultRequest(limit, fallback int64) int64 {
	if limit > 0 {
		return limit
	}
	return fallback
}

func ValidateResources(in *commonv1.ResourceSpec) error {
	if in.GetRequests().GetCpuMilli() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.requests.cpu_milli must be >= 0")
	}
	if in.GetRequests().GetMemoryBytes() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.requests.memory_bytes must be >= 0")
	}
	if in.GetLimits().GetCpuMilli() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.limits.cpu_milli must be >= 0")
	}
	if in.GetLimits().GetMemoryBytes() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.resources.limits.memory_bytes must be >= 0")
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
	return nil
}
