package adminv1

import (
	"context"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
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

type CapabilityDiagnostics interface {
	GetNodeCapabilitySnapshot(context.Context, string) (*capabilityv1.CapabilitySnapshot, error)
	ListNodeCapabilityTransitions(context.Context, string, int32) ([]adminkernel.CapabilityTransition, error)
	ListCapabilityReconcileQueue(context.Context, string, int32) ([]adminkernel.CapabilityReconcileItem, error)
	GetAllocationCapabilityDiagnostics(context.Context, string) (*adminkernel.AllocationCapabilityDiagnostics, error)
}

type Access interface {
	CreatePrincipal(ctx context.Context, name, displayName string, kind accesskernel.PrincipalKind) (accesskernel.Principal, error)
	ListPrincipals(ctx context.Context) ([]accesskernel.Principal, error)
	DisablePrincipal(ctx context.Context, id string) (accesskernel.Principal, error)
	AddCredential(ctx context.Context, principalID, label string, der []byte) (accesskernel.Credential, error)
	ListCredentials(ctx context.Context, principalID string) ([]accesskernel.Credential, error)
	RevokeCredential(ctx context.Context, id string) (accesskernel.Credential, error)
	GrantBinding(ctx context.Context, principalID string, scope accesskernel.ScopeType, namespace string, role accesskernel.Role) (accesskernel.Binding, error)
	ListBindings(ctx context.Context, principalID, namespace string, includeRevoked bool) ([]accesskernel.Binding, error)
	RevokeBinding(ctx context.Context, id string) (accesskernel.Binding, error)
}

type Dependencies struct {
	Now                        func() time.Time
	AllocationLifecycleRetries AllocationLifecycleRetries
	AdminAuditEvents           AdminAuditEvents
	Reliability                Reliability
	Storage                    Storage
	Services                   Services
	Nodes                      Nodes
	CapabilityDiagnostics      CapabilityDiagnostics
	NodeHeartbeatWindow        time.Duration
	NodeSummaryWindow          time.Duration
	Access                     Access
}
