package adminv1

import (
	"context"
	"time"

	appaccess "github.com/cofy-x/axern/control/controld/internal/application/access"
	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
)

type AllocationLifecycleRetries interface {
	ListAllocationLifecycleRetries(ctx context.Context, filter allocationkernel.LifecycleRetryFilter, now time.Time) ([]allocationkernel.LifecycleRetryItem, error)
	ForceAllocationLifecycleRetry(ctx context.Context, req allocationkernel.ForceLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error)
	FailAllocationLifecycleRetry(ctx context.Context, req allocationkernel.FailLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error)
	ClearAllocationLifecycleRetry(ctx context.Context, req allocationkernel.ClearLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error)
}

type AdminAuditEvents interface {
	ListAdminAuditEvents(ctx context.Context, filter adminkernel.AuditEventFilter) ([]adminkernel.AuditEvent, error)
}

type Reliability interface {
	ConsistencySnapshot(ctx context.Context, now time.Time) (consistencykernel.Snapshot, error)
	Health(ctx context.Context, now time.Time) (adminkernel.ReliabilityHealth, error)
}

type Storage interface {
	ListStorageBindings(ctx context.Context, filter adminkernel.StorageBindingFilter) ([]adminkernel.StorageBinding, error)
	ListStorageReclaims(ctx context.Context, filter adminkernel.StorageReclaimFilter) ([]adminkernel.StorageReclaim, error)
	RetryStorageBinding(ctx context.Context, req adminkernel.RetryStorageBindingRequest) (*adminkernel.StorageBinding, error)
}

type Services interface {
	PurgeService(ctx context.Context, serviceID, operatorReason string, now time.Time) (string, error)
}

type Nodes interface {
	ListNodes(ctx context.Context, filter adminkernel.NodeListFilter) ([]*nodekernel.Record, error)
	RetireNode(ctx context.Context, nodeID, operatorReason string, now time.Time) (*nodekernel.Record, error)
}

type Dependencies struct {
	Now                        func() time.Time
	AllocationLifecycleRetries AllocationLifecycleRetries
	AdminAuditEvents           AdminAuditEvents
	Reliability                Reliability
	Storage                    Storage
	Services                   Services
	Nodes                      Nodes
	NodeHeartbeatWindow        time.Duration
	NodeSummaryWindow          time.Duration
	Access                     *appaccess.Service
}
