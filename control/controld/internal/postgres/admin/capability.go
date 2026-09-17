package pgadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) GetNodeCapabilitySnapshot(ctx context.Context, nodeID string) (*capabilityv1.CapabilitySnapshot, error) {
	var payload []byte
	err := s.db.Pool().QueryRow(ctx, `SELECT summary FROM node_summaries WHERE node_id = $1`, strings.TrimSpace(nodeID)).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "node capability snapshot not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load node capability snapshot: %w", err)
	}
	summary := &nodev1.NodeSummary{}
	if err := protojson.Unmarshal(payload, summary); err != nil {
		return nil, fmt.Errorf("unmarshal node summary: %w", err)
	}
	if summary.GetCapabilitySnapshot() == nil {
		return nil, grpcstatus.Error(codes.NotFound, "node capability snapshot not found")
	}
	return summary.GetCapabilitySnapshot(), nil
}

func (s *Store) GetAllocationCapabilityDiagnostics(ctx context.Context, allocationID string) (*adminkernel.AllocationCapabilityDiagnostics, error) {
	allocationID = strings.TrimSpace(allocationID)
	result := &adminkernel.AllocationCapabilityDiagnostics{AllocationID: allocationID}
	if err := s.db.Pool().QueryRow(ctx, `SELECT node_id FROM allocations WHERE allocation_id = $1`, allocationID).Scan(&result.NodeID); errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "allocation not found")
	} else if err != nil {
		return nil, fmt.Errorf("load allocation capability diagnostics: %w", err)
	}
	requirements, err := pgallocation.LoadCapabilityRequirements(ctx, s.db.Pool(), allocationID)
	if err != nil {
		return nil, fmt.Errorf("load allocation capability requirements: %w", err)
	}
	result.Requirements = requirements
	var conditionsJSON []byte
	err = s.db.Pool().QueryRow(ctx, `SELECT conditions FROM allocation_capability_conditions WHERE allocation_id = $1`, allocationID).Scan(&conditionsJSON)
	if err == nil {
		result.ConditionSet = &capabilityv1.CapabilityConditionSet{}
		if err := protojson.Unmarshal(conditionsJSON, result.ConditionSet); err != nil {
			return nil, fmt.Errorf("unmarshal allocation capability conditions: %w", err)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load allocation capability conditions: %w", err)
	}
	return result, nil
}
