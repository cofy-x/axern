package pgconsistency

import (
	"context"
	"fmt"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/jackc/pgx/v5"
)

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type dependentResource string

const (
	dependentResourceLease  dependentResource = "active_lease"
	dependentResourceTunnel dependentResource = "active_tunnel"

	defaultIssueLimit = 200
)

func Snapshot(ctx context.Context, q queryer, now time.Time) (consistencykernel.Snapshot, error) {
	return SnapshotWithLimit(ctx, q, now, defaultIssueLimit)
}

func SnapshotWithLimit(ctx context.Context, q queryer, now time.Time, issueLimit int) (consistencykernel.Snapshot, error) {
	if issueLimit <= 0 {
		issueLimit = defaultIssueLimit
	}
	counts, err := loadCounts(ctx, q, now)
	if err != nil {
		return consistencykernel.Snapshot{}, err
	}
	issues := make([]consistencykernel.Issue, 0)
	truncated := false
	loaders := []func(context.Context, queryer, time.Time, int) ([]consistencykernel.Issue, bool, error){
		loadActiveReservationIssues,
		loadActiveLeaseIssues,
		loadActiveTunnelIssues,
		loadServiceReferenceIssues,
	}
	for _, load := range loaders {
		remaining := issueLimit - len(issues)
		if remaining <= 0 {
			truncated = true
			break
		}
		more, moreTruncated, err := load(ctx, q, now, remaining)
		if err != nil {
			return consistencykernel.Snapshot{}, err
		}
		issues = append(issues, more...)
		if moreTruncated {
			truncated = true
			break
		}
	}
	return consistencykernel.NewSnapshot(counts, issues, truncated), nil
}

func loadCounts(ctx context.Context, q queryer, now time.Time) (consistencykernel.Counts, error) {
	var counts consistencykernel.Counts
	err := q.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM workload_reservations WHERE released_at IS NULL),
			(SELECT COUNT(*) FROM execution_leases WHERE revoked = FALSE AND expires_at > $1),
			(SELECT COUNT(*) FROM tunnel_sessions WHERE revoked = FALSE AND status IN (
				'TUNNEL_SESSION_STATUS_PENDING',
				'TUNNEL_SESSION_STATUS_RUNNING',
				'TUNNEL_SESSION_STATUS_DEGRADED'
			)),
			(SELECT COUNT(*) FROM allocation_reconcile_queue)
	`, now.UTC()).Scan(
		&counts.ActiveReservations,
		&counts.ActiveLeases,
		&counts.ActiveTunnels,
		&counts.ReconcileQueue,
	)
	if err != nil {
		return consistencykernel.Counts{}, fmt.Errorf("query consistency counts: %w", err)
	}
	return counts, nil
}

func loadActiveReservationIssues(ctx context.Context, q queryer, _ time.Time, limit int) ([]consistencykernel.Issue, bool, error) {
	rows, err := q.Query(ctx, `
		SELECT wr.allocation_id, wr.owner_type, wr.owner_id, wr.node_id,
			COALESCE(a.status, ''), COALESCE(a.owner_type, ''), COALESCE(a.owner_id, ''), COALESCE(a.node_id, '')
		FROM workload_reservations wr
		LEFT JOIN allocations a ON a.allocation_id = wr.allocation_id
		WHERE wr.released_at IS NULL
		  AND (
			a.allocation_id IS NULL
			OR a.status = ANY($1::text[])
			OR wr.owner_type <> a.owner_type
			OR wr.owner_id <> a.owner_id
			OR wr.node_id <> a.node_id
		  )
		ORDER BY wr.created_at ASC, wr.allocation_id ASC
		LIMIT $2
	`, endedAllocationStatuses(), limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("query active reservation consistency: %w", err)
	}
	defer rows.Close()

	var out []consistencykernel.Issue
	for rows.Next() {
		if len(out) == limit {
			return out, true, nil
		}
		var allocationID, reservationOwnerType, reservationOwnerID, reservationNodeID string
		var allocationStatus, allocationOwnerType, allocationOwnerID, allocationNodeID string
		if err := rows.Scan(
			&allocationID,
			&reservationOwnerType,
			&reservationOwnerID,
			&reservationNodeID,
			&allocationStatus,
			&allocationOwnerType,
			&allocationOwnerID,
			&allocationNodeID,
		); err != nil {
			return nil, false, err
		}
		issue := consistencykernel.Issue{
			Severity:     consistencykernel.SeverityError,
			AllocationID: allocationID,
			OwnerType:    reservationOwnerType,
			OwnerID:      reservationOwnerID,
			NodeID:       reservationNodeID,
			Status:       allocationStatus,
		}
		switch {
		case allocationStatus == "":
			issue.Code = consistencykernel.IssueActiveReservationMissingAllocation
			issue.Detail = "active reservation has no allocation row"
		case contains(endedAllocationStatuses(), allocationStatus):
			issue.Code = consistencykernel.IssueActiveReservationOnEndedAllocation
			issue.Detail = "active reservation remains after allocation ended"
		default:
			issue.Code = consistencykernel.IssueActiveReservationAllocationMismatch
			issue.Detail = fmt.Sprintf("reservation owner/node %s/%s/%s does not match allocation owner/node %s/%s/%s", reservationOwnerType, reservationOwnerID, reservationNodeID, allocationOwnerType, allocationOwnerID, allocationNodeID)
		}
		out = append(out, issue)
	}
	return out, false, rows.Err()
}

func loadActiveLeaseIssues(ctx context.Context, q queryer, now time.Time, limit int) ([]consistencykernel.Issue, bool, error) {
	rows, err := q.Query(ctx, `
		SELECT el.lease_id, el.allocation_id, el.node_id,
			COALESCE(a.status, ''), COALESCE(a.owner_type, ''), COALESCE(a.owner_id, '')
		FROM execution_leases el
		LEFT JOIN allocations a ON a.allocation_id = el.allocation_id
		WHERE el.revoked = FALSE
		  AND el.expires_at > $2
		  AND (a.allocation_id IS NULL OR a.status = ANY($1::text[]) OR el.node_id <> a.node_id)
		ORDER BY el.created_at ASC, el.allocation_id ASC
		LIMIT $3
	`, endedAllocationStatuses(), now.UTC(), limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("query active lease consistency: %w", err)
	}
	defer rows.Close()
	return scanDependentIssues(rows, dependentResourceLease, limit)
}

func loadActiveTunnelIssues(ctx context.Context, q queryer, _ time.Time, limit int) ([]consistencykernel.Issue, bool, error) {
	rows, err := q.Query(ctx, `
		SELECT ts.session_id, ts.allocation_id, ts.node_id,
			COALESCE(a.status, ''), COALESCE(a.owner_type, ''), COALESCE(a.owner_id, '')
		FROM tunnel_sessions ts
		LEFT JOIN allocations a ON a.allocation_id = ts.allocation_id
		WHERE ts.revoked = FALSE
		  AND ts.status IN (
			'TUNNEL_SESSION_STATUS_PENDING',
			'TUNNEL_SESSION_STATUS_RUNNING',
			'TUNNEL_SESSION_STATUS_DEGRADED'
		  )
		  AND (a.allocation_id IS NULL OR a.status = ANY($1::text[]) OR ts.node_id <> a.node_id)
		ORDER BY ts.created_at ASC, ts.allocation_id ASC
		LIMIT $2
	`, endedAllocationStatuses(), limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("query active tunnel consistency: %w", err)
	}
	defer rows.Close()
	return scanDependentIssues(rows, dependentResourceTunnel, limit)
}

func loadServiceReferenceIssues(ctx context.Context, q queryer, _ time.Time, limit int) ([]consistencykernel.Issue, bool, error) {
	rows, err := q.Query(ctx, `
		SELECT ref.allocation_id, s.service_id, COALESCE(a.status, ''), COALESCE(a.owner_type, ''), COALESCE(a.owner_id, '')
		FROM (
			SELECT service_id, allocation_ids, created_at
			FROM services
			WHERE status NOT IN ('SERVICE_STATUS_DELETING', 'SERVICE_STATUS_DELETED')
		) s
		CROSS JOIN LATERAL jsonb_array_elements_text(s.allocation_ids) AS ref(allocation_id)
		LEFT JOIN allocations a ON a.allocation_id = ref.allocation_id
		WHERE a.allocation_id IS NULL
		   OR a.status = ANY($1::text[])
		   OR a.owner_type <> $2
		   OR a.owner_id <> s.service_id
		ORDER BY s.created_at ASC, s.service_id ASC, ref.allocation_id ASC
		LIMIT $3
	`, endedAllocationStatuses(), allocationkernel.OwnerService, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("query service allocation references: %w", err)
	}
	defer rows.Close()

	var out []consistencykernel.Issue
	for rows.Next() {
		if len(out) == limit {
			return out, true, nil
		}
		var allocationID, serviceID, status, allocationOwnerType, allocationOwnerID string
		if err := rows.Scan(&allocationID, &serviceID, &status, &allocationOwnerType, &allocationOwnerID); err != nil {
			return nil, false, err
		}
		issue := consistencykernel.Issue{
			Severity:     consistencykernel.SeverityError,
			AllocationID: allocationID,
			OwnerType:    allocationkernel.OwnerService,
			OwnerID:      serviceID,
			Status:       status,
		}
		switch {
		case status == "":
			issue.Code = consistencykernel.IssueServiceReferenceMissingAllocation
			issue.Detail = "service references an allocation row that does not exist"
		case contains(endedAllocationStatuses(), status):
			issue.Code = consistencykernel.IssueServiceReferenceEndedAllocation
			issue.Detail = "service still references an ended allocation"
		default:
			issue.Code = consistencykernel.IssueServiceReferenceOwnerMismatch
			issue.Detail = fmt.Sprintf("service references allocation owned by %s/%s", allocationOwnerType, allocationOwnerID)
		}
		out = append(out, issue)
	}
	return out, false, rows.Err()
}

func scanDependentIssues(rows pgx.Rows, resource dependentResource, limit int) ([]consistencykernel.Issue, bool, error) {
	var out []consistencykernel.Issue
	for rows.Next() {
		if len(out) == limit {
			return out, true, nil
		}
		var dependentID, allocationID, nodeID, status, ownerType, ownerID string
		if err := rows.Scan(&dependentID, &allocationID, &nodeID, &status, &ownerType, &ownerID); err != nil {
			return nil, false, err
		}
		issue := consistencykernel.Issue{
			Severity:     consistencykernel.SeverityError,
			AllocationID: allocationID,
			OwnerType:    ownerType,
			OwnerID:      ownerID,
			NodeID:       nodeID,
			DependentID:  dependentID,
			Status:       status,
		}
		switch {
		case status == "":
			issue.Code = dependentIssueCode(resource, "missing_allocation")
			issue.Detail = string(resource) + " has no allocation row"
		case contains(endedAllocationStatuses(), status):
			issue.Code = dependentIssueCode(resource, "on_ended_allocation")
			issue.Detail = string(resource) + " remains after allocation ended"
		default:
			issue.Code = dependentIssueCode(resource, "allocation_node_mismatch")
			issue.Detail = string(resource) + " node does not match allocation node"
		}
		out = append(out, issue)
	}
	return out, false, rows.Err()
}

func dependentIssueCode(resource dependentResource, condition string) consistencykernel.IssueCode {
	switch string(resource) + ":" + condition {
	case string(dependentResourceLease) + ":missing_allocation":
		return consistencykernel.IssueActiveLeaseMissingAllocation
	case string(dependentResourceLease) + ":on_ended_allocation":
		return consistencykernel.IssueActiveLeaseOnEndedAllocation
	case string(dependentResourceLease) + ":allocation_node_mismatch":
		return consistencykernel.IssueActiveLeaseAllocationNodeMismatch
	case string(dependentResourceTunnel) + ":missing_allocation":
		return consistencykernel.IssueActiveTunnelMissingAllocation
	case string(dependentResourceTunnel) + ":on_ended_allocation":
		return consistencykernel.IssueActiveTunnelOnEndedAllocation
	case string(dependentResourceTunnel) + ":allocation_node_mismatch":
		return consistencykernel.IssueActiveTunnelAllocationNodeMismatch
	default:
		return consistencykernel.IssueCode(string(resource) + "_" + condition)
	}
}

func endedAllocationStatuses() []string {
	return []string{
		commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String(),
	}
}

func contains(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}
