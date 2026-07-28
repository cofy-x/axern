package oci

import (
	"os"
	"path/filepath"
	"strings"

	spec "github.com/opencontainers/runtime-spec/specs-go"
)

type RuntimeFilesConfig struct {
	DNS RuntimeDNSConfig
}

func DefaultRuntimeFilesConfig() RuntimeFilesConfig {
	return RuntimeFilesConfig{
		DNS: RuntimeDNSConfig{
			HostResolvConfPaths: append([]string(nil), defaultHostResolvPaths...),
			Options:             append([]string(nil), defaultResolvConfOption...),
		},
	}
}

func (c RuntimeFilesConfig) withDefaults() RuntimeFilesConfig {
	c.DNS = c.DNS.withDefaults()
	return c
}

type RuntimeDNSConfig struct {
	Nameservers         []string
	SearchDomains       []string
	Options             []string
	HostResolvConfPaths []string
}

func (c RuntimeDNSConfig) withDefaults() RuntimeDNSConfig {
	if len(c.HostResolvConfPaths) == 0 {
		c.HostResolvConfPaths = append([]string(nil), defaultHostResolvPaths...)
	}
	if len(c.Options) == 0 {
		c.Options = append([]string(nil), defaultResolvConfOption...)
	}
	return c
}

func materializeRuntimeEtcFiles(bundleDir string, ociSpec *spec.Spec, runtimeFiles RuntimeFilesConfig) error {
	if bundleDir == "" || ociSpec == nil {
		return nil
	}
	etcDir := filepath.Join(bundleDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		return err
	}

	hostname := strings.TrimSpace(ociSpec.Hostname)
	if hostname == "" {
		hostname = "axnoded"
	}

	files := []runtimeEtcFile{
		{name: "hostname", target: "/etc/hostname", content: hostname + "\n"},
		{name: "hosts", target: "/etc/hosts", content: buildHostsFile(hostname)},
	}
	if !hasMountDestination(ociSpec, "/etc/resolv.conf") {
		resolvConf, err := buildResolvConf(runtimeFiles.DNS)
		if err != nil {
			return err
		}
		files = append(files, runtimeEtcFile{name: "resolv.conf", target: "/etc/resolv.conf", content: resolvConf})
	}

	files = managedRuntimeEtcFiles(ociSpec, files)
	if len(files) == 0 {
		return nil
	}

	rootfsPath := runtimeRootfsPath(bundleDir, ociSpec)
	if shouldUseRuntimeEtcDirMount(ociSpec, rootfsPath, files) {
		managedEtcDir := filepath.Join(bundleDir, "runtime-etc")
		if err := materializeRuntimeEtcDir(rootfsPath, managedEtcDir, files); err != nil {
			return err
		}
		insertRuntimeEtcDirMount(ociSpec, managedEtcDir)
		return nil
	}

	for _, file := range files {
		path := filepath.Join(etcDir, file.name)
		if err := os.WriteFile(path, []byte(file.content), 0644); err != nil {
			return err
		}
		appendRuntimeFileMountIfAbsent(ociSpec, file.target, path)
	}
	return nil
}

func hasMountDestination(ociSpec *spec.Spec, target string) bool {
	for _, mount := range ociSpec.Mounts {
		if mount.Destination == target {
			return true
		}
	}
	return false
}

func appendRuntimeFileMountIfAbsent(ociSpec *spec.Spec, target, source string) {
	if hasMountDestination(ociSpec, target) {
		return
	}
	ociSpec.Mounts = append(ociSpec.Mounts, spec.Mount{
		Destination: target,
		Type:        "bind",
		Source:      source,
		Options:     []string{"rbind", "ro"},
	})
}
