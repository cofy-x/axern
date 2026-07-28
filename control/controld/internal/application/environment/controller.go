package appenvironment

import (
	"context"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

type Control interface {
	CreateEnvironment(ctx context.Context, spec *environmentv1.EnvironmentSpec, labels map[string]string, now time.Time) (*environmentv1.Environment, error)
	GetEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error)
	ListEnvironments(ctx context.Context, filter *environmentv1.ListFilter) ([]*environmentv1.Environment, error)
	DeleteEnvironment(ctx context.Context, id string, now time.Time) (*environmentv1.Environment, error)
}

func NewAuthoritative(catalog environmentkernel.CatalogReader, imageResolver environmentkernel.ImageResolver, secrets environmentkernel.RegistryCredentialResolver, store runkernel.EnvironmentStore) Control {
	return authoritativeEnvironmentAccess{
		catalog:       catalog,
		imageResolver: imageResolver,
		secrets:       secrets,
		store:         store,
	}
}

type authoritativeEnvironmentAccess struct {
	catalog       environmentkernel.CatalogReader
	imageResolver environmentkernel.ImageResolver
	secrets       environmentkernel.RegistryCredentialResolver
	store         runkernel.EnvironmentStore
}

func (p authoritativeEnvironmentAccess) CreateEnvironment(ctx context.Context, spec *environmentv1.EnvironmentSpec, labels map[string]string, now time.Time) (*environmentv1.Environment, error) {
	normalized, template, err := environmentkernel.ResolveSpec(ctx, spec, p.catalog, p.imageResolver, p.secrets)
	if err != nil {
		return nil, err
	}
	return p.store.CreateEnvironment(ctx, runkernel.CreateEnvironmentParams{
		Spec:     normalized,
		Template: template,
		Labels:   labels,
	}, now)
}

func (p authoritativeEnvironmentAccess) GetEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error) {
	return p.store.GetEnvironment(ctx, id)
}

func (p authoritativeEnvironmentAccess) ListEnvironments(ctx context.Context, filter *environmentv1.ListFilter) ([]*environmentv1.Environment, error) {
	return p.store.ListEnvironments(ctx, filter)
}

func (p authoritativeEnvironmentAccess) DeleteEnvironment(ctx context.Context, id string, now time.Time) (*environmentv1.Environment, error) {
	return p.store.DeleteEnvironment(ctx, id, now)
}
