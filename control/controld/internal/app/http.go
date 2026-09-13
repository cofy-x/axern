package app

import (
	"context"
	"net/http"

	"github.com/cofy-x/axern/control/controld/internal/api/debughttp"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	pgconsistency "github.com/cofy-x/axern/control/controld/internal/postgres/consistency"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
)

const debugReconcileQueueLimit = 100

func (a *App) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", debughttp.New(debughttp.Config{
		DebugNodes: func() []nodekernel.DebugNode {
			return a.registry.DebugNodes(a.now(), a.heartbeatFreshnessWindow, a.summaryFreshnessWindow)
		},
		ResourcePolicy: func() debughttp.ResourcePolicySnapshot {
			return debughttp.ResourcePolicySnapshot{
				CPUOvercommitRatio:     a.resourcePolicy.CPUOvercommitRatio,
				MemoryOvercommitPolicy: "disabled",
			}
		},
		ListEnvironmentTemplates: func(ctx context.Context) (*catalogv1.ListEnvironmentTemplatesResponse, error) {
			return a.PublicV1Handler().ListEnvironmentTemplates(ctx, &catalogv1.ListEnvironmentTemplatesRequest{})
		},
		ListNamespaceQuotas: func(ctx context.Context) (*quotav1.ListNamespaceQuotasResponse, error) {
			return a.PublicV1Handler().ListNamespaceQuotas(ctx, &quotav1.ListNamespaceQuotasRequest{})
		},
		ListReconcileQueue: func(ctx context.Context) ([]allocationkernel.LifecycleRetryItem, error) {
			return pgallocation.DebugReconcileItems(ctx, a.db.Pool(), a.now(), debugReconcileQueueLimit)
		},
		ReconcileHealth: func() reconcilekernel.HealthSnapshot {
			if a.reconcileHealth == nil {
				return reconcilekernel.EmptyHealthSnapshot()
			}
			return a.reconcileHealth.Snapshot()
		},
		ConsistencySnapshot: func(ctx context.Context) (consistencykernel.Snapshot, error) {
			return pgconsistency.Snapshot(ctx, a.db.Pool(), a.now())
		},
	}))
	return mux
}
