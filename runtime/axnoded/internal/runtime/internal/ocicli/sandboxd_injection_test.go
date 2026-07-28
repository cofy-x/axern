package ocicli

import (
	"path/filepath"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func TestResolveSandboxdInjectionDefaultsToSandboxd(t *testing.T) {
	hostBinary := filepath.Join(t.TempDir(), "axern-sandboxd")
	t.Setenv(sandboxdBinaryPathEnv, hostBinary)
	options := resolveSandboxdInjectionOptions()
	if options == nil || options.HostBinaryPath != hostBinary {
		t.Fatalf("options = %#v, want host binary %q", options, hostBinary)
	}
}

func TestResolveSandboxdInjectionUsesDefaultBinaryPath(t *testing.T) {
	t.Setenv(sandboxdBinaryPathEnv, "")
	options := resolveSandboxdInjectionOptions()
	if options == nil || options.HostBinaryPath != runtimeoci.SandboxdDefaultHostBinaryPath {
		t.Fatalf("options = %#v, want default host binary", options)
	}
}

func TestPrepareBundlePassesResolvedSandboxdInjectionToLoader(t *testing.T) {
	hostBinary := filepath.Join(t.TempDir(), "axern-sandboxd")
	t.Setenv(sandboxdBinaryPathEnv, hostBinary)
	loader := &sandboxdInjectionLoaderStub{}

	_, _, err := PrepareBundle(PrepareBundleOptions{
		Loader:        loader,
		ContainerRoot: t.TempDir(),
		RuntimeName:   "test-runtime",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
		},
		ContainerID: "sandboxd-test",
	})
	if err != nil {
		t.Fatalf("PrepareBundle() error = %v", err)
	}
	if loader.lastOptions.SandboxdInjection == nil {
		t.Fatal("SandboxdInjection was nil")
	}
	if loader.lastOptions.SandboxdInjection.HostBinaryPath != hostBinary {
		t.Fatalf("host binary = %q, want %q", loader.lastOptions.SandboxdInjection.HostBinaryPath, hostBinary)
	}
}

type sandboxdInjectionLoaderStub struct {
	lastOptions runtimeoci.LoadOptions
}

func (l *sandboxdInjectionLoaderStub) PrepareBundleTemplate(runtimeoci.TemplateOptions) (*runtimeoci.BundleTemplate, error) {
	return &runtimeoci.BundleTemplate{}, nil
}

func (l *sandboxdInjectionLoaderStub) MaterializeBundle(*runtimeoci.BundleTemplate, runtimeoci.LoadOptions) (string, *spec.Spec, error) {
	return "", nil, nil
}

func (l *sandboxdInjectionLoaderStub) Generate(options runtimeoci.LoadOptions) (string, *spec.Spec, error) {
	l.lastOptions = options
	return filepath.Join("/tmp", options.ContainerID), &spec.Spec{
		Annotations: map[string]string{"loader": "stub"},
		Process:     &spec.Process{},
	}, nil
}
