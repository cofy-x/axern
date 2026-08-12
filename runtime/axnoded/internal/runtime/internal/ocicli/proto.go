package ocicli

import (
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/protobuf/proto"
)

func CloneCreateRequestWithoutResource(request *apipb.CreateContainerRequest) *apipb.CreateContainerRequest {
	if request == nil {
		return nil
	}
	cloned := proto.Clone(request).(*apipb.CreateContainerRequest)
	cloned.Resource = nil
	return cloned
}

func CloneLinuxContainerResources(resource *apipb.LinuxContainerResources) *apipb.LinuxContainerResources {
	if resource == nil {
		return &apipb.LinuxContainerResources{}
	}
	return proto.Clone(resource).(*apipb.LinuxContainerResources)
}

func LoadSpec(baseFile string) (*spec.Spec, error) {
	return runtimeoci.LoadSpec(baseFile)
}
