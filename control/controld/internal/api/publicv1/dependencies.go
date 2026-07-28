package publicv1

import (
	"context"
	"time"

	agentprofilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/agentprofile"
	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

type CatalogReader interface {
	Get(id, version string) (*catalogv1.RuntimeTemplate, bool)
	List(req *catalogv1.ListRuntimeTemplatesRequest) []*catalogv1.RuntimeTemplate
	GetAgentBundle(id, version string) (*catalogv1.AgentBundle, bool)
	ListAgentBundles(req *catalogv1.ListAgentBundlesRequest) []*catalogv1.AgentBundle
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

type AgentProfiles interface{ agentprofilekernel.Control }

type Rollouts interface{ rolloutkernel.Store }

type Services interface {
	servicekernel.Reader
	servicekernel.Mutator
}

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
	Now            func() time.Time
	Catalog        CatalogReader
	Environments   Environments
	Secrets        Secrets
	AgentProfiles  AgentProfiles
	Rollouts       Rollouts
	Runs           Runs
	Services       Services
	ServiceWatcher servicekernel.Watcher
	Functions      functionkernel.Control
	Tunnels        tunnelkernel.Control
	Namespaces     Namespaces
	Quotas         Quotas
}
