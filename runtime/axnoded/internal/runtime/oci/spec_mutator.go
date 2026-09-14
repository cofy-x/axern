package oci

import (
	"fmt"
	"slices"
	"strings"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func appendMountIfMissing(ociSpec *spec.Spec, mnt *apipb.Mount) {
	if ociSpec == nil || mnt == nil {
		return
	}
	for _, existing := range ociSpec.Mounts {
		if existing.Destination != mnt.Target {
			continue
		}
		if existing.Type != mnt.Type || existing.Source != mnt.Source {
			continue
		}
		if strings.Join(existing.Options, ",") != strings.Join(mnt.Options, ",") {
			continue
		}
		return
	}
	ociSpec.Mounts = append(ociSpec.Mounts, spec.Mount{
		Destination: mnt.Target,
		Type:        mnt.Type,
		Source:      mnt.Source,
		Options:     append([]string(nil), mnt.Options...),
	})
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func combineEnvs(envs []string, request *apipb.CreateContainerRequest) []string {
	envMap := map[string]string{}
	for _, env := range envs {
		kv := strings.SplitN(env, "=", 2)
		if len(kv) == 2 {
			envMap[kv[0]] = kv[1]
		}
	}
	if request != nil {
		for _, env := range request.Envs {
			envMap[env.Key] = env.Value
		}
	}
	combined := make([]string, 0, len(envMap))
	for k, v := range envMap {
		combined = append(combined, fmt.Sprintf("%s=%s", k, v))
	}
	return combined
}
