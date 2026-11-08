package oci

import (
	"debug/elf"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
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

func materializeSandboxdInjection(bundleDir string, ociSpec *spec.Spec, options *SandboxdInjectionOptions, guestTaskLimit int64) error {
	if options == nil {
		return nil
	}
	if bundleDir == "" {
		return fmt.Errorf("sandboxd injection requires bundle dir")
	}
	if ociSpec == nil || ociSpec.Process == nil {
		return fmt.Errorf("sandboxd injection requires OCI process")
	}
	EnforceProcessSecurity(ociSpec.Process)
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
	appendSandboxdEnvironmentMounts(ociSpec, runtimeDir)
	ociSpec.Process.Args = []string{
		SandboxdGuestBinaryPath,
		"--socket", SandboxdGuestSocketPath,
		"--entrypoint-json", SandboxdGuestEntrypointPath,
	}
	if guestTaskLimit != 0 {
		ociSpec.Process.Args = append(ociSpec.Process.Args, "--guest-task-limit", strconv.FormatInt(guestTaskLimit, 10))
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
	out, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0555)
	if err != nil {
		return fmt.Errorf("create sandboxd binary: %w", err)
	}
	verified := false
	defer func() {
		_ = out.Close()
		if !verified {
			_ = os.Remove(path)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy sandboxd binary: %w", err)
	}
	// Verify the exact copied inode. A dynamic loader from the caller's image
	// must never run during root self-exec before child privilege reduction.
	if err := validateStaticSandboxdBinary(out); err != nil {
		return err
	}
	if err := out.Chmod(0555); err != nil {
		return fmt.Errorf("chmod sandboxd binary: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync sandboxd binary: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close sandboxd binary: %w", err)
	}
	verified = true
	return nil
}

func validateStaticSandboxdBinary(file *os.File) error {
	parsed, err := elf.NewFile(file)
	if err != nil {
		return fmt.Errorf("sandboxd must be a static Linux ELF executable: %w", err)
	}
	defer parsed.Close()
	machine := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[runtime.GOARCH]
	if machine == elf.EM_NONE || parsed.Class != elf.ELFCLASS64 || parsed.Data != elf.ELFDATA2LSB || parsed.Machine != machine ||
		(parsed.OSABI != elf.ELFOSABI_NONE && parsed.OSABI != elf.ELFOSABI_LINUX) || (parsed.Type != elf.ET_EXEC && parsed.Type != elf.ET_DYN) {
		return fmt.Errorf("sandboxd must be a static Linux ELF executable for this node architecture")
	}
	for _, program := range parsed.Progs {
		if program.Type == elf.PT_INTERP || program.Type == elf.PT_DYNAMIC {
			return fmt.Errorf("sandboxd must not use an image-provided ELF interpreter or dynamic dependencies")
		}
	}
	libraries, err := parsed.ImportedLibraries()
	if err != nil || len(libraries) != 0 {
		return fmt.Errorf("sandboxd must not import dynamic libraries")
	}
	return nil
}

func appendSandboxdEnvironmentMounts(ociSpec *spec.Spec, runtimeDir string) {
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
	if user.UID != 0 || user.GID != 0 || len(user.AdditionalGids) > 0 || user.Username != "" || user.Umask != nil {
		return fmt.Errorf("sandboxd injection currently supports only root OCI process user")
	}
	return nil
}
