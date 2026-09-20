package environment

import (
	"context"
	"strings"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type ImageResolver interface {
	Resolve(ctx context.Context, imageRef string, opts ResolveOptions) (*ResolvedImage, error)
}

type ResolvedImage struct {
	Ref        string
	Descriptor *environmentv1.OciImageDescriptor
}

type ResolveOptions struct {
	DockerConfigJSON string
}

type RegistryCredentialResolver interface {
	ResolveDockerConfigJSON(ctx context.Context, id string) (string, bool, error)
}

func ResolveSpec(ctx context.Context, spec *environmentv1.EnvironmentSpec, images ImageResolver, credentials RegistryCredentialResolver) (*environmentv1.EnvironmentSpec, *environmentv1.ResolvedEnvironmentSpec, error) {
	if spec == nil {
		return nil, nil, grpcstatus.Error(codes.InvalidArgument, "spec is required")
	}
	imageRef := strings.TrimSpace(spec.GetImage().GetRef())
	if imageRef == "" {
		return nil, nil, grpcstatus.Error(codes.InvalidArgument, "image.ref is required")
	}
	return resolveImageSpec(ctx, images, credentials, spec)
}

func resolveImageSpec(ctx context.Context, images ImageResolver, credentials RegistryCredentialResolver, spec *environmentv1.EnvironmentSpec) (*environmentv1.EnvironmentSpec, *environmentv1.ResolvedEnvironmentSpec, error) {
	if images == nil {
		return nil, nil, grpcstatus.Error(codes.FailedPrecondition, "image resolution is not configured")
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
			RootfsReadonly:       spec.GetImage().GetRootfsReadonly(),
			RegistryCredentialID: registryCredentialID,
		},
	}
	return normalized, synthesizeImageSpec(normalized, resolved.Descriptor), nil
}

func synthesizeImageSpec(spec *environmentv1.EnvironmentSpec, descriptor *environmentv1.OciImageDescriptor) *environmentv1.ResolvedEnvironmentSpec {
	image := spec.GetImage()
	return &environmentv1.ResolvedEnvironmentSpec{
		ImageDescriptor: descriptor,
		RootfsReadonly:  image.GetRootfsReadonly(),
	}
}
