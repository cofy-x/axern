package nodev1

import (
	"context"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

type NodeStore interface {
	Register(ctx context.Context, params nodekernel.RegisterParams) (*nodekernel.Record, error)
	Report(ctx context.Context, params nodekernel.ReportParams) (*nodekernel.Record, error)
	Authenticate(ctx context.Context, nodeID, nodeAuthToken string) error
}

type NodeRegistry interface {
	Register(nodeID, nodeTarget string, runtimes []string, now time.Time)
	Report(nodeID, nodeTarget string, runtimes []string, summary *controlnodev1.NodeSummary, now time.Time)
}

type NodeReporter interface {
	Report(context.Context, nodekernel.ReportParams) error
}

type AllocationControl interface {
	BatchReportAllocationStatus(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationStatusObservation, now time.Time) ([]string, error)
	BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error
	BatchReportAllocationMemoryObservations(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationMemoryObservation, now time.Time) error
	ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error
	WatchExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*commonv1.ExecutionLease, int64, error)
}

type TunnelControl interface{ tunnelkernel.NodeControl }

type Dependencies struct {
	Now                    func() time.Time
	NodeStore              NodeStore
	Registry               NodeRegistry
	Reporter               NodeReporter
	Allocations            AllocationControl
	Tunnels                TunnelControl
	NotifyServiceReconcile func(...string)
}
