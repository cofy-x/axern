package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
)

func TestRunscOverlay2ValueUsesFilestoreDir(t *testing.T) {
	handler := &RunscServiceHandler{
		filestoreDir:     "/var/lib/axnoded/filestore",
		overlayTmpfsSize: "256M",
	}

	assert.Equal(t, "root:dir=/var/lib/axnoded/filestore", handler.overlay2Value(false, true))
}

func TestRunscOverlay2ValueUsesMemoryForReadOnlyRootfsPath(t *testing.T) {
	handler := &RunscServiceHandler{
		overlayTmpfsSize: "256M",
	}

	assert.Equal(t, "root:memory,size=256M", handler.overlay2Value(false, true))
}

func TestRunscOverlay2ValueSkipsReadonlyContainerRoot(t *testing.T) {
	handler := &RunscServiceHandler{
		filestoreDir:     "/var/lib/axnoded/filestore",
		overlayTmpfsSize: "256M",
	}

	assert.Equal(t, "", handler.overlay2Value(true, true))
}

func TestRunscOverlayArgsForWritableRootfsPath(t *testing.T) {
	if goruntime.GOOS == "darwin" {
		t.Skip("path readonly detection is unsupported on darwin")
	}
	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0755))

	bundlePath := t.TempDir()
	writeRunscOverlayTestSpec(t, bundlePath, spec.Root{Path: rootfsDir})
	handler := &RunscServiceHandler{}
	args, err := handler.overlayArgsForBundle(bundlePath)
	assert.NoError(t, err)
	assert.Nil(t, args)
}

func TestRunscOverlayArgsUsesFilestoreForBundleRoot(t *testing.T) {
	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0755))

	bundlePath := t.TempDir()
	writeRunscOverlayTestSpec(t, bundlePath, spec.Root{Path: rootfsDir})
	handler := &RunscServiceHandler{filestoreDir: "/var/lib/axnoded/filestore"}

	args, err := handler.overlayArgsForBundle(bundlePath)
	assert.NoError(t, err)
	assert.Equal(t, []string{"--overlay2", "root:dir=/var/lib/axnoded/filestore"}, args)
}

func TestRunscOverlayArgsSkipsReadonlyBundleRoot(t *testing.T) {
	bundlePath := t.TempDir()
	writeRunscOverlayTestSpec(t, bundlePath, spec.Root{Path: t.TempDir(), Readonly: true})
	handler := &RunscServiceHandler{filestoreDir: "/var/lib/axnoded/filestore"}

	args, err := handler.overlayArgsForBundle(bundlePath)
	assert.NoError(t, err)
	assert.Nil(t, args)
}

func writeRunscOverlayTestSpec(t *testing.T, bundlePath string, root spec.Root) {
	t.Helper()
	data, err := json.Marshal(spec.Spec{Root: &root})
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundlePath, config.ContainerSpecFile), data, 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
}

func TestRunscCreateContainerSkipsCgroupUpdateWhenIgnoreCgroupsEnabled(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}
	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{
		Binary: writeFakeOCIRuntimeBinary(t, rootDir, "runsc"),
	}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	handler.common.SetRuntimeRunnerBinary(writeFakeRuntimeRunnerBinary(t, rootDir))
	disableSandboxReadyWait(t, handler)
	handler.ignoreCgroups = true

	request := newLocalCreateRequest(t)
	request.Runtime = config.RuntimeNameRunsc
	request.Resource = &apipb.LinuxContainerResources{
		MemoryLimitInBytes: 1024,
	}

	_, err = handler.CreateContainer(context.Background(), request, contract.HandlerOptions{
		ContainerID: "runsc-ignore-cgroups",
		CgroupPath:  "/sandbox/test",
		RootfsType:  contract.StartupRootfsTypeLocal,
	})
	if err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}

	bundlePath := filepath.Join(rootDir, "containers", "runsc-ignore-cgroups")
	ociSpec, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if ociSpec.Linux == nil {
		t.Fatalf("expected linux section in spec")
	}
	assert.Empty(t, ociSpec.Linux.CgroupsPath)
}
