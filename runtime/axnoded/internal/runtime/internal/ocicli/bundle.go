package ocicli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	startupResultOK      = "ok"
	startupResultError   = "error"
	startupRootfsUnknown = "unknown"

	sandboxdBinaryPathEnv = "AXERN_SANDBOXD_BINARY"
)

type PrepareBundleOptions struct {
	Loader                runtimeoci.Loader
	ContainerRoot         string
	RuntimeName           string
	Request               *apipb.CreateContainerRequest
	ContainerID           string
	CgroupPath            string
	RuntimeCgroupPath     string
	AdditionalAnnotations map[string]string
	ExecutionProfile      *runtimeoci.ExecutionProfile
	RootfsType            string
	BundleTemplateCarrier runtimeoci.TemplateCarrier
	BundleTemplateSource  *runtimeoci.TemplateOptions
}

func PrepareBundle(options PrepareBundleOptions) (string, *apipb.ContainerMetadata, error) {
	runtimeCgroupPath := options.CgroupPath
	if options.RuntimeCgroupPath != "" {
		runtimeCgroupPath = options.RuntimeCgroupPath
	}

	bundleOptions := runtimeoci.LoadOptions{
		ContainerID: options.ContainerID,
		Request:     options.Request,

		CgroupPath:            runtimeCgroupPath,
		AdditionalAnnotations: options.AdditionalAnnotations,
		ExecutionProfile:      options.ExecutionProfile,
	}
	bundleOptions.SandboxdInjection = resolveSandboxdInjectionOptions()

	bundlePath, specConf, err := GenerateBundle(options, bundleOptions)
	if err != nil {
		return "", nil, fmt.Errorf("generate oci failed because %v", err)
	}

	if options.Request.Stderr == "" {
		options.Request.Stderr = filepath.Join(options.ContainerRoot, options.ContainerID, "stderr.log")
	}
	if options.Request.Stdout == "" {
		options.Request.Stdout = filepath.Join(options.ContainerRoot, options.ContainerID, "stdout.log")
	}

	return bundlePath, &apipb.ContainerMetadata{
		ID:             options.ContainerID,
		RuntimeHandler: options.RuntimeName,
		Labels:         specConf.Annotations,
		Stdout:         options.Request.Stdout,
		Stderr:         options.Request.Stderr,
	}, nil
}

func resolveSandboxdInjectionOptions() *runtimeoci.SandboxdInjectionOptions {
	hostBinaryPath := strings.TrimSpace(os.Getenv(sandboxdBinaryPathEnv))
	if hostBinaryPath == "" {
		hostBinaryPath = runtimeoci.SandboxdDefaultHostBinaryPath
	}
	return &runtimeoci.SandboxdInjectionOptions{HostBinaryPath: hostBinaryPath}
}

func GenerateBundle(options PrepareBundleOptions, loadOptions runtimeoci.LoadOptions) (string, *spec.Spec, error) {
	rootfsType := options.RootfsType
	if rootfsType == "" {
		rootfsType = startupRootfsUnknown
	}

	if options.BundleTemplateCarrier == nil || options.BundleTemplateSource == nil {
		metrics.RecordBundleTemplate(options.RuntimeName, rootfsType, "miss")
		start := time.Now()
		bundlePath, specConf, err := options.Loader.Generate(loadOptions)
		result := startupResultOK
		if err != nil {
			result = startupResultError
		}
		metrics.RecordBundleMaterializeDuration(options.RuntimeName, rootfsType, result, time.Since(start).Seconds())
		return bundlePath, specConf, err
	}

	templateSource := *options.BundleTemplateSource
	if templateSource.ExecutionProfile == nil {
		templateSource.ExecutionProfile = options.ExecutionProfile
	}
	template, reused, err := options.BundleTemplateCarrier.LoadOrPrepareBundleTemplate(func() (*runtimeoci.BundleTemplate, error) {
		return options.Loader.PrepareBundleTemplate(templateSource)
	})
	if err != nil {
		metrics.RecordBundleTemplate(options.RuntimeName, rootfsType, "error")
		return "", nil, err
	}
	if reused {
		metrics.RecordBundleTemplate(options.RuntimeName, rootfsType, "hit")
	} else {
		metrics.RecordBundleTemplate(options.RuntimeName, rootfsType, "miss")
	}

	start := time.Now()
	bundlePath, specConf, err := options.Loader.MaterializeBundle(template, loadOptions)
	result := startupResultOK
	if err != nil {
		result = startupResultError
	}
	metrics.RecordBundleMaterializeDuration(options.RuntimeName, rootfsType, result, time.Since(start).Seconds())
	return bundlePath, specConf, err
}
