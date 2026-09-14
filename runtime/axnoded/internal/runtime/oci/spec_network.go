package oci

import spec "github.com/opencontainers/runtime-spec/specs-go"

func applyNetworkNamespace(ociSpec *spec.Spec, namespacePath string) {
	if ociSpec == nil || ociSpec.Linux == nil || namespacePath == "" {
		return
	}
	for idx := range ociSpec.Linux.Namespaces {
		if ociSpec.Linux.Namespaces[idx].Type == spec.NetworkNamespace {
			ociSpec.Linux.Namespaces[idx].Path = namespacePath
			return
		}
	}
	ociSpec.Linux.Namespaces = append(ociSpec.Linux.Namespaces, spec.LinuxNamespace{
		Type: spec.NetworkNamespace,
		Path: namespacePath,
	})
}
