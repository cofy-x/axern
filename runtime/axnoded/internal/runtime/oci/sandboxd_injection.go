package oci

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	spec "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	SandboxdDefaultHostBinaryPath = "/usr/local/libexec/axnoded/axern-sandboxd"
	SandboxdGuestBinaryPath       = "/mnt/axern-sandboxd"
	SandboxdGuestEntrypointPath   = "/mnt/axern-entrypoint.json"
	SandboxdGuestSocketPath       = "/mnt/axern-sandboxd.sock"

	sandboxdBundleMetadataDir    = "axern/sandboxd"
	sandboxdBundleEntrypointName = "axern-entrypoint.json"
	sandboxdBundleBinaryName     = "axern-sandboxd"
	sandboxdRuntimeDir           = "/mnt"
)

func SandboxdBundleRuntimeDir(bundleDir string) string {
	return filepath.Join(bundleDir, sandboxdBundleMetadataDir)
}

func SandboxdBundleSocketPath(bundleDir string) string {
	return filepath.Join(SandboxdBundleRuntimeDir(bundleDir), filepath.Base(SandboxdGuestSocketPath))
}

type SandboxdInjectionOptions struct {
	HostBinaryPath string
}

type sandboxdEntrypointMetadata struct {
	Args     []string  `json:"args"`
	Cwd      string    `json:"cwd,omitempty"`
	Env      []string  `json:"env,omitempty"`
	User     spec.User `json:"user"`
	Terminal bool      `json:"terminal,omitempty"`
}

func materializeSandboxdInjection(bundleDir string, ociSpec *spec.Spec, options *SandboxdInjectionOptions) error {
	if options == nil {
		return nil
	}
	if bundleDir == "" {
		return fmt.Errorf("sandboxd injection requires bundle dir")
	}
	if ociSpec == nil || ociSpec.Process == nil {
		return fmt.Errorf("sandboxd injection requires OCI process")
	}
	if len(ociSpec.Process.Args) == 0 || ociSpec.Process.Args[0] == "" {
		return fmt.Errorf("sandboxd injection requires original process args")
	}
	if err := validateSandboxdSupportedUser(ociSpec.Process.User); err != nil {
		return err
	}
	if ociSpec.Process.Terminal {
		return fmt.Errorf("sandboxd injection currently does not support OCI terminal processes")
	}
	hostBinaryPath := options.HostBinaryPath
	if hostBinaryPath == "" {
		hostBinaryPath = SandboxdDefaultHostBinaryPath
	}
	if info, err := os.Stat(hostBinaryPath); err != nil {
		return fmt.Errorf("sandboxd binary is not available at %s: %w", hostBinaryPath, err)
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("sandboxd binary %s is not a regular file", hostBinaryPath)
	}
	if err := validateSandboxdMountDestinations(ociSpec); err != nil {
		return err
	}

	runtimeDir := filepath.Join(bundleDir, sandboxdBundleMetadataDir)
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return fmt.Errorf("prepare sandboxd metadata dir: %w", err)
	}
	entrypointPath := filepath.Join(runtimeDir, sandboxdBundleEntrypointName)
	metadata := sandboxdEntrypointMetadata{
		Args:     append([]string(nil), ociSpec.Process.Args...),
		Cwd:      ociSpec.Process.Cwd,
		Env:      append([]string(nil), ociSpec.Process.Env...),
		User:     ociSpec.Process.User,
		Terminal: ociSpec.Process.Terminal,
	}
	buf, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sandboxd entrypoint metadata: %w", err)
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(entrypointPath, buf, 0600); err != nil {
		return fmt.Errorf("write sandboxd entrypoint metadata: %w", err)
	}
	binaryPath := filepath.Join(runtimeDir, sandboxdBundleBinaryName)
	if err := copySandboxdBinary(binaryPath, hostBinaryPath); err != nil {
		return err
	}
	appendSandboxdRuntimeMounts(ociSpec, runtimeDir)
	ociSpec.Process.Args = []string{
		SandboxdGuestBinaryPath,
		"--socket", SandboxdGuestSocketPath,
		"--entrypoint-json", SandboxdGuestEntrypointPath,
	}
	ociSpec.Process.Cwd = "/"
	ociSpec.Process.Env = sandboxdDaemonEnv(ociSpec.Process.Env)
	return nil
}

func sandboxdDaemonEnv(env []string) []string {
	out := append([]string(nil), env...)
	for _, item := range out {
		if strings.HasPrefix(item, "PATH=") {
			return out
		}
	}
	return append(out, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
}

func copySandboxdBinary(path, source string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale sandboxd binary: %w", err)
	}
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open sandboxd binary: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0555)
	if err != nil {
		return fmt.Errorf("create sandboxd binary: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy sandboxd binary: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close sandboxd binary: %w", closeErr)
	}
	if err := os.Chmod(path, 0555); err != nil {
		return fmt.Errorf("chmod sandboxd binary: %w", err)
	}
	return nil
}

func appendSandboxdRuntimeMounts(ociSpec *spec.Spec, runtimeDir string) {
	ociSpec.Mounts = append(ociSpec.Mounts, spec.Mount{
		Destination: sandboxdRuntimeDir,
		Type:        "bind",
		Source:      runtimeDir,
		Options:     []string{"rbind", "rw", "nosuid", "nodev"},
	})
}

func validateSandboxdMountDestinations(ociSpec *spec.Spec) error {
	for _, mount := range ociSpec.Mounts {
		if isSandboxdMountDestinationConflict(mount.Destination) {
			return fmt.Errorf("sandboxd injection mount destination %s conflicts with reserved runtime directory %s", mount.Destination, sandboxdRuntimeDir)
		}
	}
	return nil
}

func isSandboxdMountDestinationConflict(destination string) bool {
	cleaned := path.Clean(destination)
	return cleaned == sandboxdRuntimeDir || strings.HasPrefix(cleaned, sandboxdRuntimeDir+"/")
}

func validateSandboxdSupportedUser(user spec.User) error {
	if user.UID != 0 || user.GID != 0 || len(user.AdditionalGids) > 0 || user.Username != "" {
		return fmt.Errorf("sandboxd injection currently supports only root OCI process user")
	}
	return nil
}
