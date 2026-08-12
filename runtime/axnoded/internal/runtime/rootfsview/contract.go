package rootfsview

import apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"

func ImmutableMountFromProto(in *apipb.ImmutableRootfsMount) ImmutableMountDescriptor {
	if in == nil {
		return ImmutableMountDescriptor{}
	}
	return ImmutableMountDescriptor{
		Identity: in.GetIdentity(), EffectiveRoot: in.GetEffectiveRoot(), Filesystem: in.GetFilesystem(),
		BackingFilesystems: append([]string(nil), in.GetBackingFilesystems()...), LowerDirs: append([]string(nil), in.GetLowerDirs()...),
		Readonly: in.GetReadonly(), LeaseID: in.GetLeaseID(),
	}
}
