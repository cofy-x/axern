package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func TestRunscFactoryComposesRuntimeServices(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader(filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeInstanceConfig{Binary: "/usr/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}

	if handler.FileService() == nil {
		t.Fatal("expected runsc file service to be composed")
	}
	t.Cleanup(handler.ShutDown)
}

func TestRunscConfigurationBelongsToLoadedHandler(t *testing.T) {
	root := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader(filepath.Join(root, "containers"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.RuntimeInstanceConfig{Binary: "/usr/bin/runsc"}
	handler, err := NewRunscServiceHandler(config.Config{RootDir: root}, cfg, loader)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handler.ShutDown)
	before, err := handler.ConfigurationDigest()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Binary = "/unloaded/runsc"
	if err := os.WriteFile(filepath.Join(root, "runsc-config.json"), []byte(`{"process":{"env":["TERM=xterm"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := handler.ConfigurationDigest()
	if err != nil {
		t.Fatal(err)
	}
	if before != after || handler.common.Binary() != "/usr/bin/runsc" {
		t.Fatal("unloaded configuration changed handler policy or evidence")
	}
}
