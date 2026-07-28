package postgres

import (
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"google.golang.org/protobuf/proto"
)

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
