package oci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func TestMaterializeSandboxdInjectionRewritesProcessAndWritesEntrypoint(t *testing.T) {
	bundleDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	ociSpec := &spec.Spec{
		Process: &spec.Process{
			Args: []string{"/bin/sh", "-c", "echo ok"},
			Cwd:  "/work",
			Env:  []string{"A=1", "B=2"},
			User: spec.User{UID: 0, GID: 0},
		},
		Annotations: map[string]string{"existing": "true"},
	}

	err := materializeSandboxdInjection(bundleDir, ociSpec, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err != nil {
		t.Fatalf("materializeSandboxdInjection() error = %v", err)
	}
	wantArgs := []string{SandboxdGuestBinaryPath, "--socket", SandboxdGuestSocketPath, "--entrypoint-json", SandboxdGuestEntrypointPath}
	if !reflect.DeepEqual(ociSpec.Process.Args, wantArgs) {
		t.Fatalf("process args = %#v, want %#v", ociSpec.Process.Args, wantArgs)
	}
	if ociSpec.Process.Cwd != "/" {
		t.Fatalf("daemon cwd = %q, want /", ociSpec.Process.Cwd)
	}
	if !hasEnvValue(ociSpec.Process.Env, "A", "1") || !hasEnvValue(ociSpec.Process.Env, "B", "2") {
		t.Fatalf("daemon env did not preserve workload env: %v", ociSpec.Process.Env)
	}
	if !hasEnvValue(ociSpec.Process.Env, "PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin") {
		t.Fatalf("daemon env missing default PATH: %v", ociSpec.Process.Env)
	}
	runtimeDir := filepath.Join(bundleDir, sandboxdBundleMetadataDir)
	assertMountPresent(t, ociSpec.Mounts, sandboxdRuntimeDir, runtimeDir)

	entrypointPath := filepath.Join(bundleDir, sandboxdBundleMetadataDir, sandboxdBundleEntrypointName)
	var metadata sandboxdEntrypointMetadata
	data, err := os.ReadFile(entrypointPath)
	if err != nil {
		t.Fatalf("read entrypoint metadata: %v", err)
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("decode entrypoint metadata: %v", err)
	}
	if !reflect.DeepEqual(metadata.Args, []string{"/bin/sh", "-c", "echo ok"}) {
		t.Fatalf("metadata args = %#v", metadata.Args)
	}
	if metadata.Cwd != "/work" || !reflect.DeepEqual(metadata.Env, []string{"A=1", "B=2"}) {
		t.Fatalf("metadata cwd/env = %q %#v", metadata.Cwd, metadata.Env)
	}
	if metadata.User.UID != 0 || metadata.User.GID != 0 || metadata.Terminal {
		t.Fatalf("metadata did not preserve user/terminal: %#v", metadata)
	}
	binaryPath := filepath.Join(bundleDir, sandboxdBundleMetadataDir, sandboxdBundleBinaryName)
	if data, err := os.ReadFile(binaryPath); err != nil {
		t.Fatalf("read sandboxd binary: %v", err)
	} else if string(data) != "sandboxd" {
		t.Fatalf("sandboxd binary content = %q", string(data))
	}
	if info, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("stat sandboxd binary: %v", err)
	} else if info.Mode().Perm() != 0555 {
		t.Fatalf("sandboxd binary mode = %v, want 0555", info.Mode().Perm())
	}
}

func TestMaterializeSandboxdInjectionFailsWhenBinaryMissing(t *testing.T) {
	err := materializeSandboxdInjection(t.TempDir(), &spec.Spec{
		Process: &spec.Process{Args: []string{"/bin/true"}},
	}, &SandboxdInjectionOptions{HostBinaryPath: filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("materializeSandboxdInjection() error = nil, want missing binary error")
	}
}

func TestMaterializeSandboxdInjectionRejectsNonRootProcessUser(t *testing.T) {
	bundleDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{
			Args: []string{"/bin/true"},
			User: spec.User{UID: 1000, GID: 1000},
		},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err == nil {
		t.Fatal("materializeSandboxdInjection() error = nil, want non-root user error")
	}
}

func TestMaterializeSandboxdInjectionRejectsAdditionalGids(t *testing.T) {
	bundleDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{
			Args: []string{"/bin/true"},
			User: spec.User{AdditionalGids: []uint32{1000}},
		},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err == nil {
		t.Fatal("materializeSandboxdInjection() error = nil, want additional gids error")
	}
}

func TestMaterializeSandboxdInjectionRejectsUsername(t *testing.T) {
	bundleDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{
			Args: []string{"/bin/true"},
			User: spec.User{Username: "app"},
		},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err == nil {
		t.Fatal("materializeSandboxdInjection() error = nil, want username error")
	}
}

func TestMaterializeSandboxdInjectionRejectsTerminalProcess(t *testing.T) {
	bundleDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{
			Args:     []string{"/bin/true"},
			Terminal: true,
		},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err == nil {
		t.Fatal("materializeSandboxdInjection() error = nil, want terminal process error")
	}
}

func TestMaterializeSandboxdInjectionRejectsMountDestinationConflict(t *testing.T) {
	bundleDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{Args: []string{"/bin/true"}},
		Mounts: []spec.Mount{
			{Destination: sandboxdRuntimeDir, Type: "tmpfs", Source: "tmpfs"},
		},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err == nil {
		t.Fatal("materializeSandboxdInjection() error = nil, want mount conflict error")
	}
}

func TestMaterializeSandboxdInjectionAllowsWorkloadRuntimeStateMounts(t *testing.T) {
	bundleDir := t.TempDir()
	rootfsDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	ociSpec := &spec.Spec{
		Process: &spec.Process{Args: []string{"/bin/true"}},
		Mounts: []spec.Mount{
			{Destination: "/run", Type: "tmpfs", Source: "tmpfs"},
			{Destination: "/var/run", Type: "tmpfs", Source: "tmpfs"},
			{Destination: "/tmp", Type: "tmpfs", Source: "tmpfs"},
		},
		Root: &spec.Root{Path: rootfsDir},
	}
	err := materializeSandboxdInjection(bundleDir, ociSpec, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err != nil {
		t.Fatalf("materializeSandboxdInjection() error = %v", err)
	}
}

func TestMaterializeSandboxdInjectionDoesNotCreateMissingRuntimeMountpoint(t *testing.T) {
	bundleDir := t.TempDir()
	rootfsDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}

	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{Args: []string{"/bin/true"}},
		Root:    &spec.Root{Path: rootfsDir},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err != nil {
		t.Fatalf("materializeSandboxdInjection() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootfsDir, "mnt")); !os.IsNotExist(err) {
		t.Fatalf("sandboxd injection modified lower rootfs: %v", err)
	}
}

func TestMaterializeSandboxdInjectionDefersRuntimeMountpointTypeValidation(t *testing.T) {
	bundleDir := t.TempDir()
	rootfsDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootfsDir, "mnt"), []byte("not-dir"), 0644); err != nil {
		t.Fatal(err)
	}

	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{Args: []string{"/bin/true"}},
		Root:    &spec.Root{Path: rootfsDir},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err != nil {
		t.Fatalf("materializeSandboxdInjection() error = %v", err)
	}
}

func TestMaterializeSandboxdInjectionUsesExistingReadonlyRuntimeMountpoint(t *testing.T) {
	bundleDir := t.TempDir()
	rootfsDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(rootfsDir, "mnt"), 0755); err != nil {
		t.Fatal(err)
	}

	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{Args: []string{"/bin/true"}},
		Root:    &spec.Root{Path: rootfsDir, Readonly: true},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err != nil {
		t.Fatalf("materializeSandboxdInjection() error = %v", err)
	}
}

func TestMaterializeSandboxdInjectionDefersReadonlyMissingMountpointToProjection(t *testing.T) {
	bundleDir := t.TempDir()
	rootfsDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}

	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{Args: []string{"/bin/true"}},
		Root:    &spec.Root{Path: rootfsDir, Readonly: true},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err != nil {
		t.Fatalf("materializeSandboxdInjection() error = %v", err)
	}
}

func TestMaterializeSandboxdInjectionRejectsNestedMountDestinationConflict(t *testing.T) {
	bundleDir := t.TempDir()
	hostBinary := filepath.Join(bundleDir, "host-sandboxd")
	if err := os.WriteFile(hostBinary, []byte("sandboxd"), 0755); err != nil {
		t.Fatal(err)
	}
	err := materializeSandboxdInjection(bundleDir, &spec.Spec{
		Process: &spec.Process{Args: []string{"/bin/true"}},
		Mounts: []spec.Mount{
			{Destination: "/mnt/workload", Type: "bind", Source: "/tmp/workload"},
		},
	}, &SandboxdInjectionOptions{HostBinaryPath: hostBinary})
	if err == nil {
		t.Fatal("materializeSandboxdInjection() error = nil, want nested mount conflict error")
	}
}

func TestMaterializeSandboxdInjectionNoopsWhenDisabled(t *testing.T) {
	ociSpec := &spec.Spec{Process: &spec.Process{Args: []string{"/bin/true"}}}
	if err := materializeSandboxdInjection(t.TempDir(), ociSpec, nil); err != nil {
		t.Fatalf("materializeSandboxdInjection() error = %v", err)
	}
	if !reflect.DeepEqual(ociSpec.Process.Args, []string{"/bin/true"}) {
		t.Fatalf("process args changed: %#v", ociSpec.Process.Args)
	}
}
