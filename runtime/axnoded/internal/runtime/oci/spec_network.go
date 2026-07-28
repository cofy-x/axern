package oci

import (
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// NetworkNamespacePolicy controls annotation-driven network namespace injection.
type NetworkNamespacePolicy struct {
	AnnotationKey string
}

// DefaultNetworkNamespacePolicy returns the default resource-manager annotation mapping.
func DefaultNetworkNamespacePolicy() NetworkNamespacePolicy {
	return NetworkNamespacePolicy{
		AnnotationKey: resourcemanager.ResourceAnnotationKeyPrefix + string(resourcemanager.InterfaceResourceName),
	}
}

func (p NetworkNamespacePolicy) apply(ociSpec *spec.Spec, annotations map[string]string) {
	if ociSpec == nil || ociSpec.Linux == nil || len(annotations) == 0 {
		return
	}
	raw := annotations[p.AnnotationKey]
	if raw == "" {
		return
	}
	netResource := &resourcemanager.NetResource{}
	if err := netResource.FromString(raw); err != nil {
		logrus.Warnf("parse network resource annotation failed: %v", err)
		return
	}
	if netResource.NetNSPath == "" {
		return
	}
	for idx := range ociSpec.Linux.Namespaces {
		if ociSpec.Linux.Namespaces[idx].Type == spec.NetworkNamespace {
			ociSpec.Linux.Namespaces[idx].Path = netResource.NetNSPath
			return
		}
	}
	ociSpec.Linux.Namespaces = append(ociSpec.Linux.Namespaces, spec.LinuxNamespace{
		Type: spec.NetworkNamespace,
		Path: netResource.NetNSPath,
	})
}
