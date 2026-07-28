package debughttp

import (
	"context"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
)

type Config struct {
	DebugNodes           func() []nodekernel.DebugNode
	ResourcePolicy       func() ResourcePolicySnapshot
	ListRuntimeTemplates func(rctx context.Context) (*catalogv1.ListRuntimeTemplatesResponse, error)
	ListNamespaceQuotas  func(rctx context.Context) (*quotav1.ListNamespaceQuotasResponse, error)
	ListReconcileQueue   func(rctx context.Context) ([]allocationkernel.LifecycleRetryItem, error)
	ReconcileHealth      func() reconcilekernel.HealthSnapshot
	ConsistencySnapshot  func(rctx context.Context) (consistencykernel.Snapshot, error)
}

type ResourcePolicySnapshot struct {
	CPUOvercommitRatio     float64 `json:"cpu_overcommit_ratio"`
	MemoryOvercommitPolicy string  `json:"memory_overcommit_policy"`
}
