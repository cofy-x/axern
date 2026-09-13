package startplan

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
)

func ResourcesToLinux(resources *commonv1.ResourceSpec) *runtime.LinuxContainerResources {
	const (
		defaultCPUShares = uint64(512)
		cpuPeriodMicros  = uint64(100000)
	)

	res := &runtime.LinuxContainerResources{
		CpuShares: defaultCPUShares,
	}
	if resources == nil {
		return res
	}
	if cpu := resources.GetRequests().GetCpuMilli(); cpu > 0 {
		res.CpuShares = uint64(cpu * 1024 / 1000)
		if res.CpuShares < 2 {
			res.CpuShares = 2
		}
	}
	if cpuLimit := resources.GetLimits().GetCpuMilli(); cpuLimit > 0 {
		res.CpuPeriod = cpuPeriodMicros
		res.CpuQuota = cpuLimit * int64(cpuPeriodMicros) / 1000
		if res.CpuQuota < 1 {
			res.CpuQuota = 1
		}
	}
	if memLimit := resources.GetLimits().GetMemoryBytes(); memLimit > 0 {
		res.MemoryLimitInBytes = memLimit
		// OCI Swap is the total memory+swap limit. Setting it equal to the
		// memory limit maps to cgroup v2 memory.swap.max=0.
		res.MemorySwapLimitInBytes = memLimit
	}
	return res
}

func BuildStaticStartEnv(lrt *environmentcache.PreparedEnvironment, request *runtime.StartRequest) []*runtime.KeyValue {
	env := make([]*runtime.KeyValue, 0, len(lrt.RootFS.Env())+len(request.EnvironmentTemplate.Env))

	logrus.WithField("image_env_count", len(lrt.RootFS.Env())).Debug("loaded image envs")
	for _, e := range lrt.RootFS.Env() {
		if parts := strings.SplitN(e, "=", 2); len(parts) == 2 {
			env = append(env, &runtime.KeyValue{Key: parts[0], Value: parts[1]})
		}
	}
	for k, v := range request.EnvironmentTemplate.Env {
		env = append(env, &runtime.KeyValue{Key: k, Value: v})
	}
	return env
}

func BuildDynamicStartEnv(request *runtime.StartRequest) []*runtime.KeyValue {
	env := make([]*runtime.KeyValue, 0, len(request.Env)+len(request.GetSecretEnv()))
	for k, v := range request.Env {
		env = append(env, &runtime.KeyValue{Key: k, Value: v})
	}
	for _, item := range request.GetSecretEnv() {
		if item != nil {
			env = append(env, &runtime.KeyValue{Key: strings.TrimSpace(item.GetName()), Value: item.GetValue()})
		}
	}
	return env
}

func BuildStartEnv(lrt *environmentcache.PreparedEnvironment, request *runtime.StartRequest) []*runtime.KeyValue {
	env := BuildStaticStartEnv(lrt, request)
	env = append(env, BuildDynamicStartEnv(request)...)
	return env
}

func BuildStartCommand(lrt *environmentcache.PreparedEnvironment, request *runtime.StartRequest) []string {
	if request != nil && request.EnvironmentTemplate != nil && len(request.EnvironmentTemplate.Argv) > 0 {
		return append([]string(nil), request.EnvironmentTemplate.Argv...)
	}
	if lrt == nil || lrt.RootFS == nil {
		return nil
	}
	return lrt.RootFS.DefaultCommand()
}

func BuildStartCwd(lrt *environmentcache.PreparedEnvironment, request *runtime.StartRequest) string {
	if request != nil && request.EnvironmentTemplate != nil && request.EnvironmentTemplate.Cwd != "" {
		return request.EnvironmentTemplate.Cwd
	}
	if lrt == nil || lrt.RootFS == nil {
		return ""
	}
	return lrt.RootFS.WorkingDir()
}

func ValidateStartRequest(request *runtime.StartRequest) error {
	switch {
	case request == nil:
		return errord.ErrInvalidArgument
	case strings.TrimSpace(request.GetAllocationID()) == "":
		return fmt.Errorf("allocation ID is required: %w", errord.ErrInvalidArgument)
	case request.EnvironmentTemplate == nil:
		return errord.ErrInvalidArgument
	case request.EnvironmentTemplate.Rootfs == nil:
		return errord.ErrInvalidArgument
	}
	if credential := strings.TrimSpace(request.GetRegistryCredential().GetDockerConfigJson()); credential != "" {
		var dockerConfig map[string]json.RawMessage
		if err := json.Unmarshal([]byte(credential), &dockerConfig); err != nil || dockerConfig == nil {
			return fmt.Errorf("registry credential must be a JSON object: %w", errord.ErrInvalidArgument)
		}
	}
	for _, mounts := range [][]*runtime.Mount{request.GetEnvironmentTemplate().GetMounts(), request.GetMounts()} {
		for _, mount := range mounts {
			if mount == nil {
				return fmt.Errorf("sandbox mount is required: %w", errord.ErrInvalidArgument)
			}
			rawTarget := strings.TrimSpace(mount.GetTarget())
			cleanTarget := path.Clean(rawTarget)
			if rawTarget == "" || cleanTarget == "/" || !strings.HasPrefix(cleanTarget, "/") || hasParentPathElement(rawTarget) {
				return fmt.Errorf("sandbox mount target %q must be an absolute container path below /: %w", rawTarget, errord.ErrInvalidArgument)
			}
		}
	}
	for _, imageMount := range request.GetImageMounts() {
		if imageMount == nil {
			return fmt.Errorf("image mount is required: %w", errord.ErrInvalidArgument)
		}
	}
	seenEnv := make(map[string]struct{}, len(request.GetSecretEnv()))
	for _, item := range request.GetSecretEnv() {
		if item == nil || strings.TrimSpace(item.GetName()) == "" {
			return fmt.Errorf("resolved secret environment name is required: %w", errord.ErrInvalidArgument)
		}
		name := strings.TrimSpace(item.GetName())
		if _, ok := seenEnv[name]; ok {
			return fmt.Errorf("resolved secret environment %q is duplicated: %w", name, errord.ErrInvalidArgument)
		}
		seenEnv[name] = struct{}{}
	}
	seenFiles := make(map[string]struct{}, len(request.GetSecretFiles()))
	for _, item := range request.GetSecretFiles() {
		if item == nil {
			return fmt.Errorf("resolved secret file is required: %w", errord.ErrInvalidArgument)
		}
		rawPath := strings.TrimSpace(item.GetPath())
		cleanPath := path.Clean(rawPath)
		if rawPath == "" || cleanPath == "/" || !strings.HasPrefix(cleanPath, "/") || hasParentPathElement(rawPath) {
			return fmt.Errorf("resolved secret file path %q must be an absolute container path below /: %w", rawPath, errord.ErrInvalidArgument)
		}
		if _, ok := seenFiles[cleanPath]; ok {
			return fmt.Errorf("resolved secret file %q is duplicated: %w", cleanPath, errord.ErrInvalidArgument)
		}
		if item.GetMode() > 0o777 {
			return fmt.Errorf("resolved secret file %q mode exceeds 0777: %w", cleanPath, errord.ErrInvalidArgument)
		}
		seenFiles[cleanPath] = struct{}{}
	}
	for _, port := range request.GetPorts() {
		if port == nil || port.GetContainerPort() < 1 || port.GetContainerPort() > 65535 || port.GetHostPort() < 0 || port.GetHostPort() > 65535 {
			return fmt.Errorf("port specification is outside 1..65535: %w", errord.ErrInvalidArgument)
		}
		switch port.GetProtocol() {
		case commonv1.PortProtocol_PORT_PROTOCOL_UNSPECIFIED, commonv1.PortProtocol_PORT_PROTOCOL_TCP, commonv1.PortProtocol_PORT_PROTOCOL_UDP:
		default:
			return fmt.Errorf("unsupported port protocol %s: %w", port.GetProtocol(), errord.ErrInvalidArgument)
		}
	}
	switch request.GetNetwork().GetMode() {
	case commonv1.NetworkMode_NETWORK_MODE_UNSPECIFIED, commonv1.NetworkMode_NETWORK_MODE_DEFAULT, commonv1.NetworkMode_NETWORK_MODE_ISOLATED, commonv1.NetworkMode_NETWORK_MODE_HOST:
	default:
		return fmt.Errorf("unsupported network mode %s: %w", request.GetNetwork().GetMode(), errord.ErrInvalidArgument)
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

func BuildStaticStartMounts(request *runtime.StartRequest) []*runtime.Mount {
	mounts := make([]*runtime.Mount, 0, len(request.EnvironmentTemplate.Mounts))
	mounts = append(mounts, request.EnvironmentTemplate.Mounts...)
	return mounts
}

func BuildDynamicStartMounts(request *runtime.StartRequest) []*runtime.Mount {
	mounts := make([]*runtime.Mount, 0, len(request.Mounts))
	mounts = append(mounts, request.Mounts...)
	return mounts
}

func BuildStartMounts(request *runtime.StartRequest) []*runtime.Mount {
	mounts := make([]*runtime.Mount, 0, len(request.EnvironmentTemplate.Mounts)+len(request.Mounts))
	mounts = append(mounts, BuildStaticStartMounts(request)...)
	mounts = append(mounts, BuildDynamicStartMounts(request)...)
	return mounts
}

func EffectiveNetworkMode(defaultMode string, request *runtime.StartRequest) string {
	if mode := request.GetNetwork().GetMode(); mode != commonv1.NetworkMode_NETWORK_MODE_UNSPECIFIED && mode != commonv1.NetworkMode_NETWORK_MODE_DEFAULT {
		return strings.ToLower(strings.TrimPrefix(mode.String(), "NETWORK_MODE_"))
	}
	return defaultMode
}

func BuildContainerRootfs(lrt *environmentcache.PreparedEnvironment) *apipb.Rootfs {
	return &apipb.Rootfs{
		Type:           "none",
		LowerDir:       "",
		RootDir:        lrt.RootFS.Path(),
		Readonly:       lrt.Readonly,
		ImmutableMount: lrt.RootFS.ImmutableMount(),
	}
}

func BuildBundleTemplateRequest(
	lrt *environmentcache.PreparedEnvironment,
	request *runtime.StartRequest,
) *apipb.CreateContainerRequest {
	return &apipb.CreateContainerRequest{
		Command: BuildStartCommand(lrt, request),
		Rootfs:  BuildContainerRootfs(lrt),
		Mounts:  BuildStaticStartMounts(request),
		Envs:    BuildStaticStartEnv(lrt, request),
		Cwd:     BuildStartCwd(lrt, request),
	}
}

func BuildBundleTemplateRequestFromPreparedEnvironment(lrt *environmentcache.PreparedEnvironment) *apipb.CreateContainerRequest {
	return &apipb.CreateContainerRequest{
		Command: append([]string(nil), lrt.Argv...),
		Rootfs:  BuildContainerRootfs(lrt),
		Mounts:  CloneEnvironmentMounts(lrt.Mounts),
		Envs:    BuildStaticRuntimeEnv(lrt),
		Cwd:     lrt.Cwd,
	}
}

func BuildCreateContainerRequest(
	lrt *environmentcache.PreparedEnvironment,
	request *runtime.StartRequest,
	env []*runtime.KeyValue,
) *apipb.CreateContainerRequest {
	resources := request.GetResources()
	return &apipb.CreateContainerRequest{
		Command:                      BuildStartCommand(lrt, request),
		Rootfs:                       BuildContainerRootfs(lrt),
		Resource:                     ResourcesToLinux(request.Resources),
		Mounts:                       BuildStartMounts(request),
		Envs:                         env,
		Stdout:                       request.Stdout,
		Stderr:                       request.Stderr,
		Cwd:                          BuildStartCwd(lrt, request),
		ID:                           request.AllocationID,
		EphemeralStorageRequestBytes: resources.GetRequests().GetEphemeralStorageBytes(),
		EphemeralStorageLimitBytes:   resources.GetLimits().GetEphemeralStorageBytes(),
	}
}

func BuildStaticRuntimeEnv(lrt *environmentcache.PreparedEnvironment) []*runtime.KeyValue {
	env := make([]*runtime.KeyValue, 0, len(lrt.RootFS.Env())+len(lrt.Env))
	for _, e := range lrt.RootFS.Env() {
		if parts := strings.SplitN(e, "=", 2); len(parts) == 2 {
			env = append(env, &runtime.KeyValue{Key: parts[0], Value: parts[1]})
		}
	}
	for k, v := range lrt.Env {
		env = append(env, &runtime.KeyValue{Key: k, Value: v})
	}
	return env
}

func KeyValuesFromStringMap(values map[string]string) []*runtime.KeyValue {
	if len(values) == 0 {
		return nil
	}
	env := make([]*runtime.KeyValue, 0, len(values))
	for k, v := range values {
		env = append(env, &runtime.KeyValue{Key: k, Value: v})
	}
	return env
}

func CloneEnvironmentMounts(input []*runtime.Mount) []*runtime.Mount {
	if len(input) == 0 {
		return nil
	}
	out := make([]*runtime.Mount, 0, len(input))
	for _, mount := range input {
		if mount == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &runtime.Mount{
			Target:  mount.Target,
			Type:    mount.Type,
			Source:  mount.Source,
			Options: append([]string(nil), mount.Options...),
		})
	}
	return out
}
