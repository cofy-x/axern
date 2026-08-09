package pgrun

import (
	"context"
	"strings"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) currentAllocation(ctx context.Context, tx pgx.Tx, allocationID string) (*runkernel.AllocationRecord, error) {
	var alloc runkernel.AllocationRecord
	var dependenciesJSON []byte
	if err := tx.QueryRow(ctx, `
		SELECT a.allocation_id, a.node_id, n.node_target, a.attempt, a.capability_dependencies
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.allocation_id = $1
	`, strings.TrimSpace(allocationID)).Scan(&alloc.AllocationID, &alloc.NodeID, &alloc.NodeTarget, &alloc.Attempt, &dependenciesJSON); err != nil {
		return nil, err
	}
	set := &capabilityv1.CapabilityDependencySet{}
	if err := protojson.Unmarshal(dependenciesJSON, set); err != nil {
		return nil, err
	}
	alloc.CapabilityDependencies = set.GetDependencies()
	return &alloc, nil
}

func (s *Store) runByAllocation(ctx context.Context, tx pgx.Tx, allocationID string) (*runv1.Run, error) {
	return scanRun(tx.QueryRow(ctx, runSelectSQL()+` WHERE allocation_id = $1`, strings.TrimSpace(allocationID)))
}
