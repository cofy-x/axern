package oci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/jsonutil"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

type Loader interface {
	PrepareBundleTemplate(options TemplateOptions) (*BundleTemplate, error)
	MaterializeBundle(template *BundleTemplate, options LoadOptions) (string, *spec.Spec, error)
	Generate(options LoadOptions) (string, *spec.Spec, error)
}

type TemplateCarrier interface {
	LoadOrPrepareBundleTemplate(func() (*BundleTemplate, error)) (*BundleTemplate, bool, error)
	ClearBundleTemplate()
}

type BundleTemplate struct {
	spec    *spec.Spec
	profile ExecutionProfile
}

type TemplateOptions struct {
	Request          *apipb.CreateContainerRequest
	ExecutionProfile *ExecutionProfile
}

type LoadOptions struct {
	ContainerID string
	Request     *apipb.CreateContainerRequest

	CgroupPath            string
	OverrideBundleDir     string
	OverrideRootPath      string
	AdditionalAnnotations map[string]string
	ExecutionProfile      *ExecutionProfile
	SandboxdInjection     *SandboxdInjectionOptions
}

type BundleLoader struct {
	baseSpec        *spec.Spec
	bundleParentDir string
	specBuilder     specBuilder
	runtimeFiles    RuntimeFilesConfig
}

// BundleLoaderOption customizes BundleLoader construction.
type BundleLoaderOption func(*bundleLoaderConfig)

type bundleLoaderConfig struct {
	profile      ExecutionProfile
	runtimeFiles RuntimeFilesConfig
}

func defaultBundleLoaderConfig() bundleLoaderConfig {
	return bundleLoaderConfig{
		profile:      DefaultExecutionProfile(),
		runtimeFiles: DefaultRuntimeFilesConfig(),
	}
}

// WithExecutionProfile configures the OCI spec policy profile used by the loader.
func WithExecutionProfile(profile ExecutionProfile) BundleLoaderOption {
	return func(config *bundleLoaderConfig) {
		config.profile = profile
	}
}

// WithRuntimeDNSConfig configures the DNS materialized into generated OCI bundles.
func WithRuntimeDNSConfig(dns RuntimeDNSConfig) BundleLoaderOption {
	return func(config *bundleLoaderConfig) {
		config.runtimeFiles.DNS = dns.withDefaults()
	}
}

// NewBundleLoader creates a loader that materializes OCI bundle specs under bundleDir.
func NewBundleLoader(baseFile, bundleDir string, options ...BundleLoaderOption) (*BundleLoader, error) {
	loaderConfig := defaultBundleLoaderConfig()
	for _, option := range options {
		if option != nil {
			option(&loaderConfig)
		}
	}

	bs := defaultBundleSpec()
	if baseFile != "" {
		bst, err := LoadSpec(baseFile)
		if err != nil {
			return nil, fmt.Errorf("load configured OCI base spec %q: %w", baseFile, err)
		}
		bs = bst
	}

	if _, err := os.Stat(bundleDir); os.IsNotExist(err) {
		if err = os.MkdirAll(bundleDir, 0755); err != nil {
			return nil, err
		}
	}
	return &BundleLoader{
		baseSpec:        bs,
		bundleParentDir: bundleDir,
		specBuilder:     newSpecBuilder(loaderConfig.profile),
		runtimeFiles:    loaderConfig.runtimeFiles.withDefaults(),
	}, nil
}

func (r *BundleLoader) PrepareBundleTemplate(options TemplateOptions) (*BundleTemplate, error) {
	if options.Request == nil {
		logrus.Debug("invalid template options: request is nil")
		return nil, errord.ErrInvalidArgument
	}

	profile := r.effectiveProfile(options.ExecutionProfile)
	ociSpec, err := r.specBuilder.withProfile(profile).build(r.baseSpec, buildOptions{request: options.Request})
	if err != nil {
		return nil, err
	}
	return &BundleTemplate{spec: ociSpec, profile: profile}, nil
}

func (r *BundleLoader) MaterializeBundle(template *BundleTemplate, options LoadOptions) (string, *spec.Spec, error) {
	if options.OverrideBundleDir == "" && options.ContainerID == "" {
		logrus.Debug("invalid materialize options: container id is empty")
		return "", nil, errord.ErrInvalidArgument
	}
	if template == nil || template.spec == nil {
		logrus.Debug("invalid materialize options: template is nil")
		return "", nil, errord.ErrInvalidArgument
	}

	profile := r.effectiveProfile(options.ExecutionProfile)
	if options.ExecutionProfile == nil {
		profile = template.profile.withDefaults()
	}
	ociSpec, err := r.specBuilder.withProfile(profile).build(template.spec, buildOptions{
		request:               options.Request,
		containerID:           options.ContainerID,
		cgroupPath:            options.CgroupPath,
		additionalAnnotations: options.AdditionalAnnotations,
		overrideRootPath:      options.OverrideRootPath,
	})
	if err != nil {
		return "", ociSpec, err
	}

	bundleDir := filepath.Join(r.bundleParentDir, options.ContainerID)
	if options.OverrideBundleDir != "" {
		bundleDir = options.OverrideBundleDir
	}
	if err := materializeRuntimeEtcFiles(bundleDir, ociSpec, r.runtimeFiles); err != nil {
		return "", ociSpec, err
	}
	if err := materializeSandboxdInjection(bundleDir, ociSpec, options.SandboxdInjection); err != nil {
		return "", ociSpec, err
	}
	ociFile := filepath.Join(bundleDir, config.ContainerSpecFile)
	if err := os.MkdirAll(filepath.Dir(ociFile), 0755); err != nil {
		return "", ociSpec, err
	}

	buf, _ := jsonutil.UnescapedMarshal(ociSpec)
	logrus.WithFields(logrus.Fields{
		"container_id": options.ContainerID,
		"spec_path":    ociFile,
		"spec_bytes":   len(buf),
	}).Debug("wrote OCI spec")
	return bundleDir, ociSpec, atomicWriteFile(ociFile, buf, 0644)
}

func (r *BundleLoader) effectiveProfile(profile *ExecutionProfile) ExecutionProfile {
	if profile == nil {
		return r.specBuilder.profile
	}
	return profile.withDefaults()
}

func (r *BundleLoader) Generate(options LoadOptions) (string, *spec.Spec, error) {
	if options.Request == nil {
		logrus.Debug("invalid options: request is nil")
		return "", nil, errord.ErrInvalidArgument
	}

	template, err := r.PrepareBundleTemplate(TemplateOptions{
		Request:          options.Request,
		ExecutionProfile: options.ExecutionProfile,
	})
	if err != nil {
		return "", nil, err
	}
	return r.MaterializeBundle(template, options)
}

func LoadSpec(baseFile string) (*spec.Spec, error) {
	specData, err := os.ReadFile(baseFile)
	if err != nil {
		return nil, err
	}
	var ociSpec spec.Spec
	if err = json.Unmarshal(specData, &ociSpec); err != nil {
		return nil, err
	}
	return &ociSpec, nil
}

func WriteSpecAtomic(target string, ociSpec *spec.Spec) error {
	buf, err := jsonutil.UnescapedMarshal(ociSpec)
	if err != nil {
		return err
	}
	return atomicWriteFile(target, buf, 0644)
}
