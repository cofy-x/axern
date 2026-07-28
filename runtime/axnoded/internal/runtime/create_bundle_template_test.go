package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func TestRuncCreateContainerUsesBundleTemplateCarrier(t *testing.T) {
	rootDir := t.TempDir()
	loader := &trackingBundleLoader{rootDir: rootDir}
	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{
		Binary: writeFakeOCIRuntimeBinary(t, rootDir, "runc"),
	}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	handler.common.SetRuntimeRunnerBinary(writeFakeRuntimeRunnerBinary(t, rootDir))
	disableSandboxReadyWait(t, handler)
	handler.ignoreCgroups = true

	carrier := &templateCarrierSpy{}
	profile := runtimeoci.DefaultExecutionProfile()
	profile.RuntimeBaseline.NoFileLimit = 2097152
	templateSource := &runtimeoci.TemplateOptions{Request: newLocalCreateRequest(t)}
	for idx := 0; idx < 2; idx++ {
		_, err := handler.CreateContainer(context.Background(), newLocalCreateRequest(t), contract.HandlerOptions{
			ContainerID:           fmt.Sprintf("runc-template-%d", idx),
			RootfsType:            contract.StartupRootfsTypeLocal,
			BundleTemplateCarrier: carrier,
			BundleTemplateSource:  templateSource,
			AdditionalAnnotations: map[string]string{"test": "true"},
			ExecutionProfile:      &profile,
		})
		if err != nil {
			t.Fatalf("CreateContainer(%d) error = %v", idx, err)
		}
	}

	if loader.prepareCalls != 1 {
		t.Fatalf("prepare calls = %d, want 1", loader.prepareCalls)
	}
	if loader.materializeCalls != 2 {
		t.Fatalf("materialize calls = %d, want 2", loader.materializeCalls)
	}
	if loader.generateCalls != 0 {
		t.Fatalf("generate calls = %d, want 0", loader.generateCalls)
	}
	if loader.lastTemplateExecutionProfile == nil || loader.lastTemplateExecutionProfile.RuntimeBaseline.NoFileLimit != 2097152 {
		t.Fatalf("template execution profile = %#v, want nofile limit 2097152", loader.lastTemplateExecutionProfile)
	}
	if loader.lastLoadExecutionProfile == nil || loader.lastLoadExecutionProfile.RuntimeBaseline.NoFileLimit != 2097152 {
		t.Fatalf("load execution profile = %#v, want nofile limit 2097152", loader.lastLoadExecutionProfile)
	}
}

func TestRunscCreateContainerUsesBundleTemplateCarrier(t *testing.T) {
	rootDir := t.TempDir()
	loader := &trackingBundleLoader{rootDir: rootDir}
	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{
		Binary: writeFakeOCIRuntimeBinary(t, rootDir, "runsc"),
	}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	handler.filestoreDir = filepath.Join(rootDir, "filestore")
	handler.common.SetRuntimeRunnerBinary(writeFakeRuntimeRunnerBinary(t, rootDir))
	disableSandboxReadyWait(t, handler)
	handler.ignoreCgroups = true

	carrier := &templateCarrierSpy{}
	templateSource := &runtimeoci.TemplateOptions{Request: newLocalCreateRequest(t)}
	for idx := 0; idx < 2; idx++ {
		_, err := handler.CreateContainer(context.Background(), newLocalCreateRequest(t), contract.HandlerOptions{
			ContainerID:           fmt.Sprintf("runsc-template-%d", idx),
			RootfsType:            contract.StartupRootfsTypeLocal,
			BundleTemplateCarrier: carrier,
			BundleTemplateSource:  templateSource,
			AdditionalAnnotations: map[string]string{"test": "true"},
		})
		if err != nil {
			t.Fatalf("CreateContainer(%d) error = %v", idx, err)
		}
	}

	if loader.prepareCalls != 1 {
		t.Fatalf("prepare calls = %d, want 1", loader.prepareCalls)
	}
	if loader.materializeCalls != 2 {
		t.Fatalf("materialize calls = %d, want 2", loader.materializeCalls)
	}
	if loader.generateCalls != 0 {
		t.Fatalf("generate calls = %d, want 0", loader.generateCalls)
	}
}

type templateCarrierSpy struct {
	template *runtimeoci.BundleTemplate
}

func (s *templateCarrierSpy) LoadOrPrepareBundleTemplate(prepare func() (*runtimeoci.BundleTemplate, error)) (*runtimeoci.BundleTemplate, bool, error) {
	if s.template != nil {
		return s.template, true, nil
	}
	template, err := prepare()
	if err != nil {
		return nil, false, err
	}
	s.template = template
	return template, false, nil
}

func (s *templateCarrierSpy) ClearBundleTemplate() {
	s.template = nil
}

type trackingBundleLoader struct {
	rootDir                      string
	prepareCalls                 int
	materializeCalls             int
	generateCalls                int
	lastTemplateExecutionProfile *runtimeoci.ExecutionProfile
	lastLoadExecutionProfile     *runtimeoci.ExecutionProfile
}

func (l *trackingBundleLoader) PrepareBundleTemplate(options runtimeoci.TemplateOptions) (*runtimeoci.BundleTemplate, error) {
	l.prepareCalls++
	l.lastTemplateExecutionProfile = options.ExecutionProfile
	return &runtimeoci.BundleTemplate{}, nil
}

func (l *trackingBundleLoader) MaterializeBundle(template *runtimeoci.BundleTemplate, options runtimeoci.LoadOptions) (string, *spec.Spec, error) {
	l.materializeCalls++
	l.lastLoadExecutionProfile = options.ExecutionProfile
	bundleDir := filepath.Join(l.rootDir, "containers", options.ContainerID)
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return "", nil, err
	}
	rootfsDir := tRootfsDir(bundleDir)
	if err := os.MkdirAll(rootfsDir, 0755); err != nil {
		return "", nil, err
	}
	ociSpec := &spec.Spec{
		Annotations: map[string]string{"loader": "tracking"},
		Process:     &spec.Process{},
		Root:        &spec.Root{Path: rootfsDir},
		Linux:       &spec.Linux{},
	}
	data, err := json.Marshal(ociSpec)
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(bundleDir, config.ContainerSpecFile), data, 0644); err != nil {
		return "", nil, err
	}
	return bundleDir, ociSpec, nil
}

func (l *trackingBundleLoader) Generate(options runtimeoci.LoadOptions) (string, *spec.Spec, error) {
	l.generateCalls++
	return "", nil, fmt.Errorf("unexpected fallback generate")
}

func tRootfsDir(bundleDir string) string {
	return filepath.Join(bundleDir, "rootfs")
}
