package pgrun

import (
	"context"
	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"strings"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5"
)

func (s *Store) currentAllocation(ctx context.Context, tx pgx.Tx, allocationID string) (*runkernel.AllocationRecord, error) {
	var alloc runkernel.AllocationRecord
	var workspaceJSON []byte
	if err := tx.QueryRow(ctx, `
		SELECT a.allocation_id, a.node_id, n.node_target,r.workspace_binding
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
        JOIN runs r ON r.run_id=a.run_id
		WHERE a.allocation_id = $1
	`, strings.TrimSpace(allocationID)).Scan(&alloc.AllocationID, &alloc.NodeID, &alloc.NodeTarget, &workspaceJSON); err != nil {
		return nil, err
	}
	binding := &nodelifecyclev1.WorkspaceBinding{}
	if err := protojson.Unmarshal(workspaceJSON, binding); err != nil {
		return nil, err
	}
	if binding.GetSpec() != nil {
		alloc.Workspace = binding
	}
	dependencies, err := pgallocation.LoadCapabilityRequirements(ctx, tx, allocationID)
	if err != nil {
		return nil, err
	}
	alloc.CapabilityRequirements = dependencies
	return &alloc, nil
}

func (s *Store) runByAllocation(ctx context.Context, tx pgx.Tx, allocationID string) (*runv1.Run, error) {
	return scanRun(tx.QueryRow(ctx, runSelectSQL()+` WHERE a.allocation_id = $1`, strings.TrimSpace(allocationID)))
}
