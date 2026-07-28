package bundleflow

import (
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func PrepareLaunchBundle(
	loader runtimeoci.Loader,
	containerRoot string,
	runtimeName string,
	request *apipb.CreateContainerRequest,
	options contract.HandlerOptions,
) (string, *apipb.ContainerMetadata, error) {
	bundleStart := time.Now()
	bundlePath, metaData, err := PrepareBundle(loader, containerRoot, runtimeName, request, options)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeBundleMaterial, time.Since(bundleStart))
	options.RecordStartupPhase(contract.StartupPhaseRuntimeBundle, time.Since(bundleStart))
	return bundlePath, metaData, err
}

func PrepareEnvelopeBundle(
	loader runtimeoci.Loader,
	containerRoot string,
	runtimeName string,
	request *apipb.CreateContainerRequest,
	options contract.HandlerOptions,
) (string, *apipb.ContainerMetadata, error) {
	return PrepareBundle(loader, containerRoot, runtimeName, request, options)
}

func PrepareBundle(
	loader runtimeoci.Loader,
	containerRoot string,
	runtimeName string,
	request *apipb.CreateContainerRequest,
	options contract.HandlerOptions,
) (string, *apipb.ContainerMetadata, error) {
	return ocicli.PrepareBundle(ocicli.PrepareBundleOptions{
		Loader:                loader,
		ContainerRoot:         containerRoot,
		RuntimeName:           runtimeName,
		Request:               request,
		ContainerID:           options.ContainerID,
		CgroupPath:            options.CgroupPath,
		RuntimeCgroupPath:     options.RuntimeCgroupPath,
		AdditionalAnnotations: options.AdditionalAnnotations,
		ExecutionProfile:      options.ExecutionProfile,
		RootfsType:            options.RootfsType,
		BundleTemplateCarrier: options.BundleTemplateCarrier,
		BundleTemplateSource:  options.BundleTemplateSource,
	})
}
