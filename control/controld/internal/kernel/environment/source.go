package environment

import (
	"context"
	"strings"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type CatalogReader interface {
	Get(id, version string) (*catalogv1.RuntimeTemplate, bool)
}

type ImageResolver interface {
	Resolve(ctx context.Context, imageRef string, opts ResolveOptions) (*ResolvedImage, error)
}

type ResolvedImage struct {
	Ref        string
	Descriptor *catalogv1.OciImageDescriptor
}

type ResolveOptions struct {
	DockerConfigJSON string
}

type RegistryCredentialResolver interface {
	ResolveDockerConfigJSON(ctx context.Context, id string) (string, bool, error)
}

func ResolveSpec(ctx context.Context, spec *environmentv1.EnvironmentSpec, catalog CatalogReader, images ImageResolver, credentials RegistryCredentialResolver) (*environmentv1.EnvironmentSpec, *catalogv1.RuntimeTemplate, error) {
	if spec == nil {
		return nil, nil, grpcstatus.Error(codes.InvalidArgument, "spec is required")
	}
	templateID := strings.TrimSpace(spec.GetTemplateID())
	imageRef := strings.TrimSpace(spec.GetImage().GetRef())
	switch {
	case templateID != "" && imageRef != "":
		return nil, nil, grpcstatus.Error(codes.InvalidArgument, "exactly one of template_id or image.ref must be set")
	case templateID == "" && imageRef == "":
		return nil, nil, grpcstatus.Error(codes.InvalidArgument, "one of template_id or image.ref is required")
	}
	if templateID != "" {
		return resolveTemplateSpec(catalog, spec)
	}
	return resolveImageSpec(ctx, images, credentials, spec)
}

func resolveTemplateSpec(catalog CatalogReader, spec *environmentv1.EnvironmentSpec) (*environmentv1.EnvironmentSpec, *catalogv1.RuntimeTemplate, error) {
	templateID := strings.TrimSpace(spec.GetTemplateID())
	if strings.TrimSpace(spec.GetImage().GetRegistryCredentialID()) != "" {
		return nil, nil, grpcstatus.Error(codes.InvalidArgument, "image.registry_credential_id is only valid with image.ref")
	}
	if spec.GetImage().GetRootfsReadonly() {
		return nil, nil, grpcstatus.Error(codes.InvalidArgument, "image.rootfs_readonly is only valid with image.ref")
	}
	template, ok := catalog.Get(templateID, spec.GetTemplateVersion())
	if !ok {
		return nil, nil, grpcstatus.Errorf(codes.NotFound, "runtime template %q not found", templateID)
	}
	if strings.TrimSpace(template.GetImageDescriptor().GetDigest()) == "" {
		return nil, nil, grpcstatus.Errorf(codes.FailedPrecondition, "runtime template %q is not digest pinned", templateID)
	}
	normalized := &environmentv1.EnvironmentSpec{
		Namespace:       NormalizeNamespace(spec.GetNamespace()),
		TemplateID:      templateID,
		TemplateVersion: template.GetVersion(),
	}
	return normalized, template, nil
}

func resolveImageSpec(ctx context.Context, images ImageResolver, credentials RegistryCredentialResolver, spec *environmentv1.EnvironmentSpec) (*environmentv1.EnvironmentSpec, *catalogv1.RuntimeTemplate, error) {
	if images == nil {
		return nil, nil, grpcstatus.Error(codes.FailedPrecondition, "image resolution is not configured")
	}
	if strings.TrimSpace(spec.GetTemplateVersion()) != "" {
		return nil, nil, grpcstatus.Error(codes.InvalidArgument, "template_version is only valid with template_id")
	}
	if strings.TrimSpace(spec.GetImage().GetDigest()) != "" {
		return nil, nil, grpcstatus.Error(codes.InvalidArgument, "image.digest is output-only")
	}
	registryCredentialID := strings.TrimSpace(spec.GetImage().GetRegistryCredentialID())
	opts := ResolveOptions{}
	if registryCredentialID != "" {
		if credentials == nil {
			return nil, nil, grpcstatus.Error(codes.FailedPrecondition, "registry credential resolution is not configured")
		}
		dockerConfigJSON, ok, err := credentials.ResolveDockerConfigJSON(ctx, registryCredentialID)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, grpcstatus.Errorf(codes.NotFound, "registry credential %q not found", registryCredentialID)
		}
		opts.DockerConfigJSON = dockerConfigJSON
	}
	resolved, err := images.Resolve(ctx, spec.GetImage().GetRef(), opts)
	if err != nil {
		return nil, nil, grpcstatus.Errorf(codes.InvalidArgument, "resolve image.ref: %v", err)
	}
	if resolved == nil || resolved.Descriptor == nil || strings.TrimSpace(resolved.Descriptor.GetDigest()) == "" {
		return nil, nil, grpcstatus.Error(codes.FailedPrecondition, "resolved image descriptor is missing a digest")
	}
	normalized := &environmentv1.EnvironmentSpec{
		Namespace: NormalizeNamespace(spec.GetNamespace()),
		Image: &environmentv1.EnvironmentImageSource{
			Ref:                  strings.TrimSpace(resolved.Ref),
			Digest:               strings.TrimSpace(resolved.Descriptor.GetDigest()),
			RootfsReadonly:       spec.GetImage().GetRootfsReadonly(),
			RegistryCredentialID: registryCredentialID,
		},
	}
	return normalized, synthesizeImageTemplate(normalized, resolved.Descriptor), nil
}

func synthesizeImageTemplate(spec *environmentv1.EnvironmentSpec, descriptor *catalogv1.OciImageDescriptor) *catalogv1.RuntimeTemplate {
	image := spec.GetImage()
	return &catalogv1.RuntimeTemplate{
		ID:              image.GetRef(),
		Version:         image.GetDigest(),
		ImageDescriptor: descriptor,
		RootfsReadonly:  image.GetRootfsReadonly(),
		Capabilities: &catalogv1.RuntimeTemplateCapabilities{
			SupportsExec:             true,
			SupportsExecStream:       true,
			SupportsLongLivedProcess: true,
			SupportsPorts:            true,
		},
		Description: "Image-backed runtime environment.",
	}
}
