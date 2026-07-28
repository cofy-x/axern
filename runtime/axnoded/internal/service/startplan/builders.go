package startplan

import (
	"strconv"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	runtimecore "github.com/cofy-x/axern/runtime/axnoded/internal/runtime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
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
	}
	return res
}

func BuildStaticStartEnv(lrt *langrtmanager.LanguageRuntime, request *runtime.StartRequest) []*runtime.KeyValue {
	env := make([]*runtime.KeyValue, 0, len(lrt.RootFS.Env())+len(request.RuntimeTemplate.RuntimeEnvs))

	logrus.WithField("image_env_count", len(lrt.RootFS.Env())).Debug("loaded image envs")
	for _, e := range lrt.RootFS.Env() {
		if parts := strings.SplitN(e, "=", 2); len(parts) == 2 {
			env = append(env, &runtime.KeyValue{Key: parts[0], Value: parts[1]})
		}
	}
	for k, v := range request.RuntimeTemplate.RuntimeEnvs {
		env = append(env, &runtime.KeyValue{Key: k, Value: v})
	}
	return env
}

func BuildDynamicStartEnv(request *runtime.StartRequest) []*runtime.KeyValue {
	env := make([]*runtime.KeyValue, 0, len(request.UserEnvs))
	for k, v := range request.UserEnvs {
		env = append(env, &runtime.KeyValue{Key: k, Value: v})
	}
	return env
}

func BuildStartEnv(lrt *langrtmanager.LanguageRuntime, request *runtime.StartRequest) []*runtime.KeyValue {
	env := BuildStaticStartEnv(lrt, request)
	env = append(env, BuildDynamicStartEnv(request)...)
	return env
}

func BuildStartCommand(lrt *langrtmanager.LanguageRuntime, request *runtime.StartRequest) []string {
	if request != nil && request.RuntimeTemplate != nil && len(request.RuntimeTemplate.Command) > 0 {
		return append([]string(nil), request.RuntimeTemplate.Command...)
	}
	if lrt == nil || lrt.RootFS == nil {
		return nil
	}
	return lrt.RootFS.DefaultCommand()
}

func BuildStartCwd(lrt *langrtmanager.LanguageRuntime, request *runtime.StartRequest) string {
	if request != nil && request.RuntimeTemplate != nil && request.RuntimeTemplate.Cwd != "" {
		return request.RuntimeTemplate.Cwd
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
	case request.RuntimeTemplate == nil:
		return errord.ErrInvalidArgument
	case request.RuntimeTemplate.Rootfs == nil:
		return errord.ErrInvalidArgument
	default:
		return nil
	}
}

func BuildStaticStartLabels(request *runtime.StartRequest) map[string]string {
	labels := map[string]string{
		workloadidentity.LabelKeyRuntimeID: request.RuntimeTemplate.ID,
	}
	if allocationID := strings.TrimSpace(request.ContainerID); allocationID != "" {
		labels[workloadidentity.LabelKeyAllocationID] = allocationID
	}
	return labels
}

func BuildDynamicStartLabels(request *runtime.StartRequest) map[string]string {
	labels := map[string]string{}
	extraConfig, ok := ParseExtraConfig(request.ExtraConfig)
	if !ok {
		return labels
	}
	if extraConfig.BlockNetwork {
		labels["netac-rules"] = config.NetAcBlockAll
	} else if extraConfig.CIDRAllowlist != "" {
		labels["netac-rules"] = extraConfig.CIDRAllowlist
	}
	if len(extraConfig.LinuxCapabilities) > 0 {
		caps := make([]string, 0, len(extraConfig.LinuxCapabilities))
		seen := make(map[string]struct{}, len(extraConfig.LinuxCapabilities))
		for _, capName := range extraConfig.LinuxCapabilities {
			normalized := strings.ToUpper(strings.TrimSpace(capName))
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			caps = append(caps, normalized)
		}
		if len(caps) > 0 {
			labels[runtimecore.LabelKeyLinuxCapabilities] = strings.Join(caps, ",")
		}
	}
	if extraConfig.AllocationAttempt > 0 {
		labels[workloadidentity.LabelKeyAllocationAttempt] = strconv.FormatInt(extraConfig.AllocationAttempt, 10)
	}
	if namespace := strings.TrimSpace(extraConfig.Namespace); namespace != "" {
		labels[workloadidentity.LabelKeyNamespace] = namespace
	}
	if serviceID := strings.TrimSpace(extraConfig.ServiceID); serviceID != "" {
		labels[workloadidentity.LabelKeyServiceID] = serviceID
	}

	return labels
}

func BuildStartLabels(request *runtime.StartRequest) map[string]string {
	labels := BuildStaticStartLabels(request)
	for k, v := range BuildDynamicStartLabels(request) {
		labels[k] = v
	}
	return labels
}

func BuildStaticStartMounts(request *runtime.StartRequest) []*runtime.Mount {
	mounts := make([]*runtime.Mount, 0, len(request.RuntimeTemplate.Mounts))
	mounts = append(mounts, request.RuntimeTemplate.Mounts...)
	return mounts
}

func BuildDynamicStartMounts(request *runtime.StartRequest) []*runtime.Mount {
	mounts := make([]*runtime.Mount, 0, len(request.Mounts))
	mounts = append(mounts, request.Mounts...)
	return mounts
}

func BuildStartMounts(request *runtime.StartRequest) []*runtime.Mount {
	mounts := make([]*runtime.Mount, 0, len(request.RuntimeTemplate.Mounts)+len(request.Mounts))
	mounts = append(mounts, BuildStaticStartMounts(request)...)
	mounts = append(mounts, BuildDynamicStartMounts(request)...)
	return mounts
}

func EffectiveNetworkMode(defaultMode string, request *runtime.StartRequest) string {
	if request.Network != "" {
		return request.Network
	}
	return defaultMode
}

func BuildContainerRootfs(lrt *langrtmanager.LanguageRuntime) *apipb.Rootfs {
	return &apipb.Rootfs{
		Type:     "none",
		LowerDir: "",
		RootDir:  lrt.RootFS.Path(),
		Readonly: lrt.Readonly,
	}
}

func BuildBundleTemplateRequest(
	lrt *langrtmanager.LanguageRuntime,
	request *runtime.StartRequest,
) *apipb.CreateContainerRequest {
	return &apipb.CreateContainerRequest{
		Runtime: request.RuntimeTemplate.Sandbox,
		Command: BuildStartCommand(lrt, request),
		Rootfs:  BuildContainerRootfs(lrt),
		Mounts:  BuildStaticStartMounts(request),
		Envs:    BuildStaticStartEnv(lrt, request),
		Labels:  BuildStaticStartLabels(request),
		Cwd:     BuildStartCwd(lrt, request),
	}
}

func BuildBundleTemplateRequestFromLanguageRuntime(lrt *langrtmanager.LanguageRuntime) *apipb.CreateContainerRequest {
	return &apipb.CreateContainerRequest{
		Runtime: lrt.Sandbox,
		Command: append([]string(nil), lrt.Command...),
		Rootfs:  BuildContainerRootfs(lrt),
		Mounts:  CloneRuntimeMounts(lrt.Mounts),
		Envs:    BuildStaticRuntimeEnv(lrt),
		Labels:  map[string]string{workloadidentity.LabelKeyRuntimeID: lrt.ID},
		Cwd:     lrt.Cwd,
	}
}

func BuildCreateContainerRequest(
	lrt *langrtmanager.LanguageRuntime,
	request *runtime.StartRequest,
	labels map[string]string,
	env []*runtime.KeyValue,
	networkMode string,
) *apipb.CreateContainerRequest {
	return &apipb.CreateContainerRequest{
		Runtime:  request.RuntimeTemplate.Sandbox,
		Command:  BuildStartCommand(lrt, request),
		Rootfs:   BuildContainerRootfs(lrt),
		Resource: ResourcesToLinux(request.Resources),
		Mounts:   BuildStartMounts(request),
		Envs:     env,
		Network:  networkMode,
		Labels:   labels,
		Stdout:   request.Stdout,
		Stderr:   request.Stderr,
		CkptDir:  request.CkptDir,
		Cwd:      BuildStartCwd(lrt, request),
		ID:       request.ContainerID,
	}
}

func BuildStaticRuntimeEnv(lrt *langrtmanager.LanguageRuntime) []*runtime.KeyValue {
	env := make([]*runtime.KeyValue, 0, len(lrt.RootFS.Env())+len(lrt.RuntimeEnvs))
	for _, e := range lrt.RootFS.Env() {
		if parts := strings.SplitN(e, "=", 2); len(parts) == 2 {
			env = append(env, &runtime.KeyValue{Key: parts[0], Value: parts[1]})
		}
	}
	for k, v := range lrt.RuntimeEnvs {
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

func BuildExecutionEnvelopeRequest(lrt *langrtmanager.LanguageRuntime, defaultNetworkMode string) *apipb.CreateContainerRequest {
	return &apipb.CreateContainerRequest{
		Runtime:  lrt.Sandbox,
		Command:  append([]string(nil), lrt.Command...),
		Rootfs:   BuildContainerRootfs(lrt),
		Resource: ResourcesToLinux(nil),
		Mounts:   CloneRuntimeMounts(lrt.Mounts),
		Envs:     BuildStaticRuntimeEnv(lrt),
		Network:  defaultNetworkMode,
		Labels:   map[string]string{workloadidentity.LabelKeyRuntimeID: lrt.ID},
		Cwd:      lrt.Cwd,
	}
}

func CloneRuntimeMounts(input []*runtime.Mount) []*runtime.Mount {
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
