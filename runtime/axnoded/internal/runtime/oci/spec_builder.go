package oci

import (
	"encoding/json"
	"path/filepath"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

type buildOptions struct {
	request *apipb.CreateContainerRequest

	containerID           string
	cgroupPath            string
	overrideRootPath      string
	additionalAnnotations map[string]string
}

type specBuilder struct {
	profile ExecutionProfile
}

func newSpecBuilder(profile ExecutionProfile) specBuilder {
	return specBuilder{
		profile: profile.withDefaults(),
	}
}

func (b specBuilder) withProfile(profile ExecutionProfile) specBuilder {
	b.profile = profile.withDefaults()
	return b
}

func (b specBuilder) build(base *spec.Spec, options buildOptions) (*spec.Spec, error) {
	ociSpec := deepCopySpec(base)
	if err := b.applyRequestToSpec(ociSpec, options); err != nil {
		return ociSpec, err
	}
	return ociSpec, nil
}

func (b specBuilder) applyRequestToSpec(ociSpec *spec.Spec, options buildOptions) error {
	if ociSpec == nil {
		return nil
	}
	ensureLinuxSpec(ociSpec)
	ociSpec.Linux.CgroupsPath = options.cgroupPath

	request := options.request
	if request != nil {
		applyProcessOverrides(ociSpec, request)
		applyMountOverrides(ociSpec, request)
		applyRootfsOverride(ociSpec, request)
		ociSpec.Annotations = combineAnnotations(ociSpec.Annotations, request.Labels)
		applyWritableLayerAnnotation(ociSpec, request)
		setSpecResource(ociSpec, request.Resource)
	}

	ociSpec.Annotations = combineAnnotations(ociSpec.Annotations, options.additionalAnnotations)
	explicitHostname := requestedHostnameAnnotation(request, options.additionalAnnotations)
	delete(ociSpec.Annotations, runtimeHostnameAnnotationKey())
	applyHostname(ociSpec, request, options.containerID, explicitHostname)
	b.profile.RuntimeBaseline.apply(ociSpec)
	b.profile.NetworkNamespace.apply(ociSpec, options.additionalAnnotations)
	b.profile.Capabilities.apply(ociSpec, ociSpec.Annotations)
	b.profile.Resources.apply(ociSpec)

	if options.overrideRootPath != "" && ociSpec.Root != nil {
		ociSpec.Root.Path = options.overrideRootPath
	}
	return validateProcessArgs(ociSpec)
}

const writableLayerAnnotationKey = "io.axnoded.resource/writable-layer"

func applyWritableLayerAnnotation(ociSpec *spec.Spec, request *apipb.CreateContainerRequest) {
	if request.GetWritableLayerRequestBytes() <= 0 {
		return
	}
	if ociSpec.Annotations == nil {
		ociSpec.Annotations = make(map[string]string)
	}
	value, _ := json.Marshal(struct {
		RequestBytes int64 `json:"request_bytes"`
		LimitBytes   int64 `json:"limit_bytes"`
	}{request.GetWritableLayerRequestBytes(), request.GetWritableLayerLimitBytes()})
	ociSpec.Annotations[writableLayerAnnotationKey] = string(value)
}

func ensureLinuxSpec(ociSpec *spec.Spec) {
	if ociSpec.Linux != nil {
		return
	}
	ociSpec.Linux = &spec.Linux{
		Namespaces: []spec.LinuxNamespace{},
	}
}

func applyProcessOverrides(ociSpec *spec.Spec, request *apipb.CreateContainerRequest) {
	if ociSpec.Process == nil {
		return
	}
	if request.Cwd != "" {
		ociSpec.Process.Cwd = request.Cwd
	}
	if len(request.Command) > 0 {
		ociSpec.Process.Args = append([]string(nil), request.Command...)
	}
	if len(request.Envs) > 0 {
		ociSpec.Process.Env = combineEnvs(ociSpec.Process.Env, request)
	}
}

func applyMountOverrides(ociSpec *spec.Spec, request *apipb.CreateContainerRequest) {
	for _, mnt := range request.Mounts {
		appendMountIfMissing(ociSpec, mnt)
	}
}

func applyRootfsOverride(ociSpec *spec.Spec, request *apipb.CreateContainerRequest) {
	if request.Rootfs == nil {
		return
	}
	if ociSpec.Root == nil {
		ociSpec.Root = &spec.Root{}
	}
	ociSpec.Root.Path = filepath.Join(request.Rootfs.RootDir)
	ociSpec.Root.Readonly = request.Rootfs.GetReadonly()
}

func deepCopySpec(in *spec.Spec) *spec.Spec {
	if in == nil {
		return nil
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out spec.Spec
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil
	}
	return &out
}
