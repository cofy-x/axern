package oci

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	spec "github.com/opencontainers/runtime-spec/specs-go"
)

type RuntimeFilesConfig struct {
	DNS RuntimeDNSConfig
}

type runtimeEtcFile struct {
	name    string
	target  string
	content string
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
	hostname := strings.TrimSpace(ociSpec.Hostname)
	if hostname == "" {
		hostname = "axnoded"
	}

	files := make([]runtimeEtcFile, 0, 3)
	if !mountDestinationsOwn(ociSpec.Mounts, "/etc/hostname") {
		files = append(files, runtimeEtcFile{name: "hostname", target: "/etc/hostname", content: hostname + "\n"})
	}
	if !mountDestinationsOwn(ociSpec.Mounts, "/etc/hosts") {
		sandboxIP, err := sandboxIPFromSpec(ociSpec)
		if err != nil {
			return err
		}
		files = append(files, runtimeEtcFile{name: "hosts", target: "/etc/hosts", content: buildHostsFile(hostname, sandboxIP)})
	}
	if !mountDestinationsOwn(ociSpec.Mounts, "/etc/resolv.conf") {
		resolvConf, err := buildResolvConf(runtimeFiles.DNS)
		if err != nil {
			return err
		}
		files = append(files, runtimeEtcFile{name: "resolv.conf", target: "/etc/resolv.conf", content: resolvConf})
	}

	if len(files) == 0 {
		return nil
	}

	runtimeFilesDir := filepath.Join(bundleDir, "sandbox-files")
	if err := os.RemoveAll(runtimeFilesDir); err != nil {
		return fmt.Errorf("remove stale sandbox files: %w", err)
	}
	if err := os.MkdirAll(runtimeFilesDir, 0755); err != nil {
		return fmt.Errorf("create sandbox files directory: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.RemoveAll(runtimeFilesDir)
		}
	}()

	for _, file := range files {
		source := filepath.Join(runtimeFilesDir, file.name)
		if err := atomicWriteFile(source, []byte(file.content), 0644); err != nil {
			return fmt.Errorf("write sandbox file %s: %w", file.name, err)
		}
		appendRuntimeFileMountIfAbsent(ociSpec, file.target, source)
	}
	succeeded = true
	return nil
}

func mountDestinationsOwn(mounts []spec.Mount, target string) bool {
	for _, mount := range mounts {
		if mountDestinationOwns(mount.Destination, target) {
			return true
		}
	}
	return false
}

func mountDestinationOwns(destination, target string) bool {
	destination = path.Clean(strings.TrimSpace(destination))
	target = path.Clean(strings.TrimSpace(target))
	if !path.IsAbs(destination) || !path.IsAbs(target) {
		return false
	}
	return destination == "/" || destination == target || strings.HasPrefix(target, destination+"/")
}

func appendRuntimeFileMountIfAbsent(ociSpec *spec.Spec, target, source string) {
	if mountDestinationsOwn(ociSpec.Mounts, target) {
		return
	}
	ociSpec.Mounts = append(ociSpec.Mounts, spec.Mount{
		Destination: target,
		Type:        "bind",
		Source:      source,
		Options:     []string{"rbind", "ro"},
	})
}
