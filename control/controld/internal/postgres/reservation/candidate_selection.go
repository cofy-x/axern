package reservation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func lockCandidateNodes(ctx context.Context, tx pgx.Tx, candidates []*placementkernel.Candidate) (map[string]*nodekernel.Record, error) {
	nodeIDs := candidateNodeIDs(candidates)
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.registered_at, n.updated_at,
		       n.retired_at, n.retired_reason, s.summary,
		       COALESCE((SELECT ARRAY_AGG(r.runtime_name ORDER BY r.runtime_name) FROM node_runtime_sets r WHERE r.node_id = n.node_id), ARRAY[]::text[])
		FROM nodes n
		LEFT JOIN node_summaries s ON s.node_id = n.node_id
		WHERE n.node_id = ANY($1::text[])
		ORDER BY n.node_id
		FOR UPDATE OF n
	`, nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("lock placement candidates: %w", err)
	}
	defer rows.Close()

	locked := make(map[string]*nodekernel.Record, len(nodeIDs))
	for rows.Next() {
		var record nodekernel.Record
		var summaryJSON []byte
		var retiredAt *time.Time
		if err := rows.Scan(&record.NodeID, &record.NodeTarget, &record.Lifecycle, &record.RegisteredAt, &record.UpdatedAt, &retiredAt, &record.RetiredReason, &summaryJSON, &record.Runtimes); err != nil {
			return nil, fmt.Errorf("scan locked placement candidate: %w", err)
		}
		if retiredAt != nil {
			record.RetiredAt = *retiredAt
		}
		if len(summaryJSON) > 0 {
			record.Summary = &nodev1.NodeSummary{}
			if err := protojson.Unmarshal(summaryJSON, record.Summary); err != nil {
				return nil, fmt.Errorf("unmarshal locked node summary: %w", err)
			}
		}
		locked[record.NodeID] = &record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked placement candidates: %w", err)
	}
	return locked, nil
}

func candidateNodeIDs(candidates []*placementkernel.Candidate) []string {
	seen := make(map[string]struct{}, len(candidates))
	nodeIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.Record == nil {
			continue
		}
		nodeID := strings.TrimSpace(candidate.NodeID)
		if nodeID == "" {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	return nodeIDs
}

type nodeReservationUsage struct {
	resources     resourcekernel.Claim
	allocationIDs []string
}

// effectiveReservationUsage closes the gap between control-plane reservation
// release and node-local cgroup cleanup. A retiring cgroup remains committed
// until axnoded has reclaimed and removed it, even when the allocation has
// already reached a terminal control-plane state.
func effectiveReservationUsage(summary *nodev1.NodeSummary, database resourcekernel.Claim) resourcekernel.Claim {
	local := summary.GetMemoryBudget().GetLocalCommitmentBytes()
	if local > database.MemoryBytes {
		database.MemoryBytes = local
	}
	return database
}

func activeCandidateReservationUsage(ctx context.Context, tx pgx.Tx, locked map[string]*nodekernel.Record) (map[string]nodeReservationUsage, error) {
	if len(locked) == 0 {
		return nil, nil
	}
	nodeIDs := make([]string, 0, len(locked))
	for nodeID := range locked {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	rows, err := tx.Query(ctx, `
		SELECT node_id, COALESCE(SUM(cpu_milli), 0), COALESCE(SUM(sandbox_memory_request_bytes), 0), COALESCE(SUM(ephemeral_storage_bytes), 0),
		       ARRAY_AGG(allocation_id ORDER BY allocation_id)
		FROM workload_reservations
		WHERE node_id = ANY($1::text[]) AND released_at IS NULL
		GROUP BY node_id
	`, nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("sum placement candidate reservations: %w", err)
	}
	defer rows.Close()

	usage := make(map[string]nodeReservationUsage, len(locked))
	for rows.Next() {
		var nodeID string
		var used nodeReservationUsage
		if err := rows.Scan(&nodeID, &used.resources.CPUMilli, &used.resources.MemoryBytes, &used.resources.EphemeralStorageBytes, &used.allocationIDs); err != nil {
			return nil, fmt.Errorf("scan placement candidate reservations: %w", err)
		}
		usage[nodeID] = used
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate placement candidate reservations: %w", err)
	}
	return usage, nil
}

func refreshPlacementCandidate(candidate *placementkernel.Candidate, record *nodekernel.Record, reserved resourcekernel.Claim, reservedAllocationIDs []string, now time.Time) *placementkernel.Candidate {
	evaluation := &nodev1.PlacementCandidate{}
	if candidate.Evaluation != nil {
		evaluation = proto.Clone(candidate.Evaluation).(*nodev1.PlacementCandidate)
	}
	evaluation.NodeID = record.NodeID
	evaluation.HeartbeatAgeSecs = nodekernel.HeartbeatAgeSecs(record.UpdatedAt, now)
	if evaluation.Rank == nil {
		evaluation.Rank = &nodev1.PlacementRank{}
	}
	resources := record.Summary.GetResources()
	evaluation.Rank.AxnodedActiveInstances = nodekernel.CalculateRuntimeSlotOccupancy(record.Summary, reservedAllocationIDs).Occupied
	evaluation.Rank.AxnodedUsedMilli = resourcekernel.SaturatingAdd(resources.GetAxnodedUsedMilli(), positiveDifference(reserved.CPUMilli, resources.GetAxnodedCommittedMilli()))
	evaluation.Rank.AxnodedUsedBytes = resourcekernel.SaturatingAdd(resources.GetAxnodedUsedBytes(), positiveDifference(reserved.MemoryBytes, record.Summary.GetMemoryBudget().GetLocalCommitmentBytes()))
	return &placementkernel.Candidate{Record: record, Evaluation: evaluation, BaseRequest: candidate.BaseRequest, Request: candidate.Request}
}

func positiveDifference(total, reported int64) int64 {
	if total <= reported {
		return 0
	}
	return total - reported
}

func allocatableFromSummary(summary *nodev1.NodeSummary) *commonv1.ResourceQuantity {
	if summary.GetAllocatable() != nil {
		return summary.GetAllocatable()
	}
	if summary.GetCapacity() != nil {
		return summary.GetCapacity()
	}
	return &commonv1.ResourceQuantity{}
}
