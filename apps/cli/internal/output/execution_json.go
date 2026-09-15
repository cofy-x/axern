package output

import (
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type ExecutionConfigJSON struct {
	Argv                            []string                              `json:"argv,omitempty"`
	Env                             map[string]string                     `json:"env,omitempty"`
	Cwd                             string                                `json:"cwd,omitempty"`
	Resources                       *ResourceSpecJSON                     `json:"resources,omitempty"`
	Network                         *NetworkSpecJSON                      `json:"network,omitempty"`
	ExtensionCapabilityRequirements []*ExtensionCapabilityRequirementJSON `json:"extension_capability_requirements,omitempty"`
	Placement                       *PlacementConstraintsJSON             `json:"placement,omitempty"`
	SecretEnv                       []*SecretEnvVarJSON                   `json:"secret_env,omitempty"`
	SecretFiles                     []*SecretFileJSON                     `json:"secret_files,omitempty"`
	ImageMounts                     []*ImageMountJSON                     `json:"image_mounts,omitempty"`
}

type ResourceQuantityJSON struct {
	CPUMilli              int64 `json:"cpu_milli"`
	MemoryBytes           int64 `json:"memory_bytes"`
	EphemeralStorageBytes int64 `json:"ephemeral_storage_bytes"`
}

type ResourceSpecJSON struct {
	Requests *ResourceQuantityJSON `json:"requests,omitempty"`
	Limits   *ResourceQuantityJSON `json:"limits,omitempty"`
}

type NetworkSpecJSON struct {
	Mode string `json:"mode"`
}

type ExtensionCapabilityRequirementJSON struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type PlacementConstraintsJSON struct {
	NodeSelector map[string]string `json:"node_selector,omitempty"`
}

type SecretEnvVarJSON struct {
	Name     string `json:"name"`
	SecretID string `json:"secret_id"`
	Key      string `json:"key"`
	Optional bool   `json:"optional,omitempty"`
}

type SecretFileJSON struct {
	Path     string `json:"path"`
	SecretID string `json:"secret_id"`
	Key      string `json:"key"`
	Mode     uint32 `json:"mode,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

type ImageMountJSON struct {
	Image    string `json:"image"`
	Target   string `json:"target"`
	Readonly bool   `json:"readonly"`
}

func NewExecutionConfigJSON(config *commonv1.ExecutionConfig) *ExecutionConfigJSON {
	if config == nil {
		return nil
	}
	return &ExecutionConfigJSON{
		Argv:                            append([]string(nil), config.GetArgv()...),
		Env:                             cloneStringMap(config.GetEnv()),
		Cwd:                             config.GetCwd(),
		Resources:                       newResourceSpecJSON(config.GetResources()),
		Network:                         newNetworkSpecJSON(config.GetNetwork()),
		ExtensionCapabilityRequirements: newExtensionCapabilityRequirementJSONs(config.GetExtensionCapabilityRequirements()),
		Placement:                       newPlacementConstraintsJSON(config.GetPlacement()),
		SecretEnv:                       newSecretEnvVarJSONs(config.GetSecretEnv()),
		SecretFiles:                     newSecretFileJSONs(config.GetSecretFiles()),
		ImageMounts:                     newImageMountJSONs(config.GetImageMounts()),
	}
}

func newResourceSpecJSON(resources *commonv1.ResourceSpec) *ResourceSpecJSON {
	if resources == nil {
		return nil
	}
	out := &ResourceSpecJSON{
		Requests: newResourceQuantityJSON(resources.GetRequests()),
		Limits:   newResourceQuantityJSON(resources.GetLimits()),
	}
	if out.Requests == nil && out.Limits == nil {
		return nil
	}
	return out
}

func newResourceQuantityJSON(quantity *commonv1.ResourceQuantity) *ResourceQuantityJSON {
	if quantity == nil {
		return nil
	}
	return &ResourceQuantityJSON{CPUMilli: quantity.GetCpuMilli(), MemoryBytes: quantity.GetMemoryBytes(), EphemeralStorageBytes: quantity.GetEphemeralStorageBytes()}
}

func newNetworkSpecJSON(network *commonv1.NetworkSpec) *NetworkSpecJSON {
	if network == nil {
		return nil
	}
	return &NetworkSpecJSON{Mode: networkModeLabel(network.GetMode())}
}

func newExtensionCapabilityRequirementJSONs(requirements []*capabilityv1.ExtensionCapabilityRequirement) []*ExtensionCapabilityRequirementJSON {
	if len(requirements) == 0 {
		return nil
	}
	out := make([]*ExtensionCapabilityRequirementJSON, 0, len(requirements))
	for _, requirement := range requirements {
		if requirement == nil {
			continue
		}
		out = append(out, &ExtensionCapabilityRequirementJSON{
			Name:  requirement.GetCapability().GetName(),
			Value: requirement.GetCapability().GetValue(),
		})
	}
	return out
}

func newPlacementConstraintsJSON(placement *commonv1.PlacementConstraints) *PlacementConstraintsJSON {
	if placement == nil {
		return nil
	}
	return &PlacementConstraintsJSON{NodeSelector: cloneStringMap(placement.GetNodeSelector())}
}

func newSecretEnvVarJSONs(vars []*commonv1.SecretEnvVar) []*SecretEnvVarJSON {
	if len(vars) == 0 {
		return nil
	}
	out := make([]*SecretEnvVarJSON, 0, len(vars))
	for _, v := range vars {
		if v == nil {
			continue
		}
		out = append(out, &SecretEnvVarJSON{
			Name:     v.GetName(),
			SecretID: v.GetSecretID(),
			Key:      v.GetKey(),
			Optional: v.GetOptional(),
		})
	}
	return out
}

func newSecretFileJSONs(files []*commonv1.SecretFile) []*SecretFileJSON {
	if len(files) == 0 {
		return nil
	}
	out := make([]*SecretFileJSON, 0, len(files))
	for _, file := range files {
		if file == nil {
			continue
		}
		out = append(out, &SecretFileJSON{
			Path:     file.GetPath(),
			SecretID: file.GetSecretID(),
			Key:      file.GetKey(),
			Mode:     file.GetMode(),
			Optional: file.GetOptional(),
		})
	}
	return out
}

func newImageMountJSONs(mounts []*commonv1.ImageMount) []*ImageMountJSON {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]*ImageMountJSON, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		out = append(out, &ImageMountJSON{
			Image:    mount.GetImage(),
			Target:   mount.GetTarget(),
			Readonly: mount.GetReadonly(),
		})
	}
	return out
}

func networkModeLabel(mode commonv1.NetworkMode) string {
	switch mode {
	case commonv1.NetworkMode_NETWORK_MODE_DEFAULT:
		return "default"
	case commonv1.NetworkMode_NETWORK_MODE_ISOLATED:
		return "isolated"
	case commonv1.NetworkMode_NETWORK_MODE_HOST:
		return "host"
	default:
		return "unspecified"
	}
}
