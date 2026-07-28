package agent

import (
	"context"
	"fmt"
	"strings"

	appcatalog "github.com/cofy-x/axern/apps/cli/internal/application/catalog"
	"github.com/cofy-x/axern/lib/go/agentbundle"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
)

const imageRefAnnotationKey = "org.opencontainers.image.ref.name"

type bundleRuntime struct {
	ID          string
	Version     string
	Image       string
	MountTarget string
	BinDir      string
	Binary      string
}

func resolveAgentBundle(ctx context.Context, catalog appcatalog.RuntimeCatalogClient, adapter Adapter) (bundleRuntime, error) {
	if catalog == nil {
		return bundleRuntime{}, fmt.Errorf("runtime catalog client is required")
	}
	id := adapter.BundleID()
	resp, err := catalog.GetAgentBundle(ctx, &catalogv1.GetAgentBundleRequest{ID: id})
	if err != nil {
		return bundleRuntime{}, err
	}
	bundle := resp.GetAgentBundle()
	if bundle == nil {
		return bundleRuntime{}, fmt.Errorf("agent bundle %q was not returned", id)
	}
	image := strings.TrimSpace(bundle.GetImageDescriptor().GetAnnotations()[imageRefAnnotationKey])
	if image == "" {
		return bundleRuntime{}, fmt.Errorf("agent bundle %q image ref is missing", id)
	}
	mountTarget := agentbundle.MountTarget(id)
	binary := agentbundle.MountedBinary(mountTarget, bundle.GetBinaryPath())
	if binary == "" {
		return bundleRuntime{}, fmt.Errorf("agent bundle %q binary path %q is invalid", id, bundle.GetBinaryPath())
	}
	return bundleRuntime{
		ID: id, Version: bundle.GetVersion(), Image: image, MountTarget: mountTarget,
		BinDir: agentbundle.BinDir(mountTarget), Binary: binary,
	}, nil
}

func interactiveShellWithBundlePath(bundle bundleRuntime) string {
	prelude := "export PATH=" + shellQuote(bundle.BinDir) + ":\"$PATH\"; exec /bin/bash -i"
	return DefaultRemoteShell + "c " + shellQuote(prelude)
}
