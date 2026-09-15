package output

import environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"

type ResolvedEnvironmentSpecJSON struct {
	RootfsReadonly   bool                    `json:"rootfs_readonly,omitempty"`
	ImageDefaultArgv []string                `json:"image_default_argv,omitempty"`
	DefaultCwd       string                  `json:"default_cwd,omitempty"`
	DefaultEnv       map[string]string       `json:"default_env,omitempty"`
	Mounts           []*EnvironmentMountJSON `json:"mounts,omitempty"`
	ImageDescriptor  *OciImageDescriptorJSON `json:"image_descriptor,omitempty"`
}

type EnvironmentMountJSON struct {
	Type    string   `json:"type,omitempty"`
	Source  string   `json:"source,omitempty"`
	Target  string   `json:"target,omitempty"`
	Options []string `json:"options,omitempty"`
}

type OciImageDescriptorJSON struct {
	Digest      string            `json:"digest,omitempty"`
	MediaType   string            `json:"media_type,omitempty"`
	SizeBytes   int64             `json:"size_bytes,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

func newResolvedEnvironmentSpecJSON(spec *environmentv1.ResolvedEnvironmentSpec) *ResolvedEnvironmentSpecJSON {
	if spec == nil {
		return nil
	}
	return &ResolvedEnvironmentSpecJSON{
		RootfsReadonly: spec.GetRootfsReadonly(), ImageDefaultArgv: append([]string(nil), spec.GetImageDefaultArgv()...),
		DefaultCwd: spec.GetDefaultCwd(), DefaultEnv: cloneStringMap(spec.GetDefaultEnv()),
		Mounts: newEnvironmentMountJSONs(spec.GetMounts()), ImageDescriptor: newOciImageDescriptorJSON(spec.GetImageDescriptor()),
	}
}

func newEnvironmentMountJSONs(mounts []*environmentv1.EnvironmentMount) []*EnvironmentMountJSON {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]*EnvironmentMountJSON, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		out = append(out, &EnvironmentMountJSON{
			Type: mount.GetType(), Source: mount.GetSource(), Target: mount.GetTarget(),
			Options: append([]string(nil), mount.GetOptions()...),
		})
	}
	return out
}

func newOciImageDescriptorJSON(descriptor *environmentv1.OciImageDescriptor) *OciImageDescriptorJSON {
	if descriptor == nil {
		return nil
	}
	return &OciImageDescriptorJSON{
		Digest: descriptor.GetDigest(), MediaType: descriptor.GetMediaType(), SizeBytes: descriptor.GetSizeBytes(),
		Annotations: cloneStringMap(descriptor.GetAnnotations()),
	}
}
