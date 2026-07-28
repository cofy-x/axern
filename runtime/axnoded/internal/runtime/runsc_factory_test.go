package runtime

import (
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func TestRunscFactoryComposesRuntimeServices(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}

	if handler.FileService() == nil {
		t.Fatal("expected runsc file service to be composed")
	}
	if handler.ProcessService() == nil {
		t.Fatal("expected runsc process service to be composed")
	}
}
