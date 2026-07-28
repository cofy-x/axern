package bundleflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func TestPrepareBundleAssignsDefaultLogs(t *testing.T) {
	rootDir := t.TempDir()
	request := &apipb.CreateContainerRequest{
		Command: []string{"/bin/sh", "-c", "exit 0"},
	}
	loader := &bundleLoaderStub{rootDir: rootDir}

	bundlePath, meta, err := PrepareBundle(loader, filepath.Join(rootDir, "containers"), "test-runtime", request, contract.HandlerOptions{
		ContainerID: "axctl-test",
	})
	if err != nil {
		t.Fatalf("PrepareBundle() error = %v", err)
	}
	if meta == nil {
		t.Fatal("PrepareBundle() returned nil metadata")
	}
	if bundlePath == "" {
		t.Fatal("PrepareBundle() returned empty bundle path")
	}
	if meta.Stdout == "" || meta.Stderr == "" {
		t.Fatalf("expected default log paths, got stdout=%q stderr=%q", meta.Stdout, meta.Stderr)
	}
	if loader.generateCalls != 1 {
		t.Fatalf("generate calls = %d, want 1", loader.generateCalls)
	}
}

type bundleLoaderStub struct {
	rootDir              string
	generateCalls        int
	lastExecutionProfile *runtimeoci.ExecutionProfile
}

func (l *bundleLoaderStub) PrepareBundleTemplate(runtimeoci.TemplateOptions) (*runtimeoci.BundleTemplate, error) {
	return &runtimeoci.BundleTemplate{}, nil
}

func (l *bundleLoaderStub) MaterializeBundle(*runtimeoci.BundleTemplate, runtimeoci.LoadOptions) (string, *spec.Spec, error) {
	return "", nil, nil
}

func (l *bundleLoaderStub) Generate(options runtimeoci.LoadOptions) (string, *spec.Spec, error) {
	l.generateCalls++
	l.lastExecutionProfile = options.ExecutionProfile
	bundleDir := filepath.Join(l.rootDir, "bundles", options.ContainerID)
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return "", nil, err
	}
	return bundleDir, &spec.Spec{
		Annotations: map[string]string{"loader": "stub"},
		Process:     &spec.Process{},
		Linux:       &spec.Linux{},
	}, nil
}

func TestPrepareBundlePropagatesExecutionProfile(t *testing.T) {
	rootDir := t.TempDir()
	request := &apipb.CreateContainerRequest{
		Command: []string{"/bin/sh", "-c", "exit 0"},
	}
	profile := runtimeoci.DefaultExecutionProfile()
	profile.RuntimeBaseline.NoFileLimit = 2097152
	loader := &bundleLoaderStub{rootDir: rootDir}

	_, _, err := PrepareBundle(loader, filepath.Join(rootDir, "containers"), "test-runtime", request, contract.HandlerOptions{
		ContainerID:      "axctl-profile",
		ExecutionProfile: &profile,
	})
	if err != nil {
		t.Fatalf("PrepareBundle() error = %v", err)
	}
	if loader.lastExecutionProfile == nil {
		t.Fatal("execution profile was not propagated to loader")
	}
	if got := loader.lastExecutionProfile.RuntimeBaseline.NoFileLimit; got != 2097152 {
		t.Fatalf("execution profile nofile limit = %d, want 2097152", got)
	}
}

func TestPrepareBundlePrefersRuntimeCgroupPath(t *testing.T) {
	rootDir := t.TempDir()
	writeFakeSandboxdBinary(t, rootDir)
	request := &apipb.CreateContainerRequest{
		Rootfs: &apipb.Rootfs{
			Type:    "local",
			RootDir: rootDir,
		},
		Command: []string{"/bin/sh"},
	}
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	bundlePath, _, err := PrepareBundle(loader, filepath.Join(rootDir, "containers"), "test-runtime", request, contract.HandlerOptions{
		ContainerID:       "runtime-cgroup-path",
		CgroupPath:        "/sandbox/test",
		RuntimeCgroupPath: "/sandbox/test/workload",
	})
	if err != nil {
		t.Fatalf("PrepareBundle() error = %v", err)
	}

	ociSpec, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if ociSpec.Linux == nil {
		t.Fatalf("expected linux section in spec")
	}
	if ociSpec.Linux.CgroupsPath != "/sandbox/test/workload" {
		t.Fatalf("cgroups path = %q, want /sandbox/test/workload", ociSpec.Linux.CgroupsPath)
	}
}

func writeFakeSandboxdBinary(t *testing.T, rootDir string) string {
	t.Helper()

	binDir := filepath.Join(rootDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	binPath := filepath.Join(binDir, "axern-sandboxd")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake sandboxd binary: %v", err)
	}
	t.Setenv("AXERN_SANDBOXD_BINARY", binPath)
	return binPath
}
