package resourceadmission

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

func lockCandidateNodes(ctx context.Context, tx pgx.Tx, candidates []*placementkernel.Candidate) (map[string]*nodekernel.Record, error) {
	nodeIDs := candidateNodeIDs(candidates)
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.admitted_at, n.last_heartbeat_at,
		       n.retired_at, n.retired_reason, s.summary
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
		var lastHeartbeatAt *time.Time
		var retiredAt *time.Time
		if err := rows.Scan(&record.NodeID, &record.NodeTarget, &record.Lifecycle, &record.AdmittedAt, &lastHeartbeatAt, &retiredAt, &record.RetiredReason, &summaryJSON); err != nil {
			return nil, fmt.Errorf("scan locked placement candidate: %w", err)
		}
		if lastHeartbeatAt != nil {
			record.LastHeartbeatAt = *lastHeartbeatAt
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

type nodeAllocationUsage struct {
	resources     resourcekernel.Claim
	allocationIDs []string
}

func activeCandidateAllocationUsage(ctx context.Context, tx pgx.Tx, locked map[string]*nodekernel.Record) (map[string]nodeAllocationUsage, error) {
	if len(locked) == 0 {
		return nil, nil
	}
	nodeIDs := make([]string, 0, len(locked))
	for nodeID := range locked {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	rows, err := tx.Query(ctx, `
		SELECT node_id, COALESCE(SUM(cpu_request_milli), 0), COALESCE(SUM(sandbox_memory_request_bytes), 0), COALESCE(SUM(ephemeral_storage_request_bytes), 0),
		       ARRAY_AGG(allocation_id ORDER BY allocation_id)
		FROM allocations
		WHERE node_id = ANY($1::text[]) AND lifecycle_state <> $2
		GROUP BY node_id
	`, nodeIDs, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String())
	if err != nil {
		return nil, fmt.Errorf("sum placement candidate allocation charges: %w", err)
	}
	defer rows.Close()

	usage := make(map[string]nodeAllocationUsage, len(locked))
	for rows.Next() {
		var nodeID string
		var used nodeAllocationUsage
		if err := rows.Scan(&nodeID, &used.resources.CPUMilli, &used.resources.MemoryBytes, &used.resources.EphemeralStorageBytes, &used.allocationIDs); err != nil {
			return nil, fmt.Errorf("scan placement candidate allocation charges: %w", err)
		}
		usage[nodeID] = used
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate placement candidate allocation charges: %w", err)
	}
	return usage, nil
}

func refreshPlacementCandidate(candidate *placementkernel.Candidate, record *nodekernel.Record, charged resourcekernel.Claim, chargedAllocationIDs []string, now time.Time) *placementkernel.Candidate {
	evaluation := placementkernel.CloneEvaluation(candidate.Evaluation)
	evaluation.NodeID = record.NodeID
	evaluation.HeartbeatAgeSecs = nodekernel.HeartbeatAgeSecs(record.LastHeartbeatAt, now)
	if evaluation.Rank == nil {
		evaluation.Rank = &placementkernel.Rank{}
	}
	evaluation.Rank.RuntimeSlotOccupancy = nodekernel.CalculateRuntimeSlotOccupancy(record.Summary, chargedAllocationIDs).Occupied
	evaluation.Rank.ChargedCPUMilli = charged.CPUMilli
	evaluation.Rank.ChargedMemoryBytes = charged.MemoryBytes
	evaluation.Rank.ChargedEphemeralBytes = charged.EphemeralStorageBytes
	return &placementkernel.Candidate{Record: record, Evaluation: evaluation, BaseRequest: candidate.BaseRequest, Request: candidate.Request}
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
