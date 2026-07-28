package storage

import (
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/proto"
)

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneRuntimeCompatibility(in *storagev1.VolumeRuntimeCompatibility) *storagev1.VolumeRuntimeCompatibility {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*storagev1.VolumeRuntimeCompatibility)
}

func cloneTopology(in *storagev1.VolumeTopology) *storagev1.VolumeTopology {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*storagev1.VolumeTopology)
}

func cloneWorkloadVolumeMount(in *privatestoragev1.WorkloadVolumeMount) *privatestoragev1.WorkloadVolumeMount {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*privatestoragev1.WorkloadVolumeMount)
}
