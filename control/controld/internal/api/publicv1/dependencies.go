package publicv1

import (
	"context"
	"time"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

type CatalogReader interface {
	Get(id, version string) (*catalogv1.EnvironmentTemplate, bool)
	List(req *catalogv1.ListEnvironmentTemplatesRequest) []*catalogv1.EnvironmentTemplate
}

type Environments interface {
	CreateEnvironment(ctx context.Context, spec *environmentv1.EnvironmentSpec, labels map[string]string, now time.Time) (*environmentv1.Environment, error)
	GetEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error)
	ListEnvironments(ctx context.Context, filter *environmentv1.ListFilter) ([]*environmentv1.Environment, error)
	DeleteEnvironment(ctx context.Context, id string, now time.Time) (*environmentv1.Environment, error)
}

type Runs interface {
	CreateRun(ctx context.Context, params runkernel.CreateParams, now time.Time) (*runv1.Run, error)
	GetRun(ctx context.Context, id string) (*runv1.Run, error)
	ListRuns(ctx context.Context, filter *runv1.RunListFilter) ([]*runv1.Run, error)
	CancelRun(ctx context.Context, runID string, now time.Time) (*runv1.Run, error)
}

type Secrets interface{ secretkernel.Control }

type Namespaces interface {
	CreateNamespace(ctx context.Context, namespace string, now time.Time) (*namespacev1.Namespace, error)
	GetNamespace(ctx context.Context, namespace string) (*namespacev1.Namespace, error)
	ListNamespaces(ctx context.Context) ([]*namespacev1.Namespace, error)
	DeleteNamespace(ctx context.Context, namespace string, now time.Time) (*namespacev1.Namespace, error)
}

type Quotas interface {
	Get(ctx context.Context, namespace string) (*quotav1.NamespaceQuota, error)
	List(ctx context.Context) ([]*quotav1.NamespaceQuota, error)
	ListEvents(ctx context.Context, namespace string, limit int) ([]*quotav1.NamespaceQuotaEvent, error)
	Set(ctx context.Context, namespace string, limits *quotav1.NamespaceQuotaLimits, now time.Time) (*quotav1.NamespaceQuota, error)
	Unset(ctx context.Context, namespace string, now time.Time) (*quotav1.NamespaceQuota, error)
}

type Dependencies struct {
	Now          func() time.Time
	Catalog      CatalogReader
	Environments Environments
	Secrets      Secrets
	Runs         Runs
	Tunnels      tunnelkernel.Control
	Namespaces   Namespaces
	Quotas       Quotas
}
