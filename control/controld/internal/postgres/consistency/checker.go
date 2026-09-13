package pgconsistency

import (
	"context"
	"fmt"
	"time"

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
			(SELECT COUNT(*) FROM reservations WHERE released_at IS NULL),
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
		SELECT a.allocation_id, a.run_id, a.node_id, a.lifecycle_state
		FROM reservations res
		JOIN allocations a ON a.allocation_id = res.allocation_id
		WHERE res.released_at IS NULL
		  AND a.lifecycle_state = ANY($1::text[])
		ORDER BY res.created_at ASC, res.allocation_id ASC
		LIMIT $2
	`, []string{commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String()}, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("query active reservation consistency: %w", err)
	}
	defer rows.Close()

	var out []consistencykernel.Issue
	for rows.Next() {
		if len(out) == limit {
			return out, true, nil
		}
		var allocationID, runID, nodeID, allocationLifecycle string
		if err := rows.Scan(&allocationID, &runID, &nodeID, &allocationLifecycle); err != nil {
			return nil, false, err
		}
		out = append(out, consistencykernel.Issue{
			Code:         consistencykernel.IssueActiveReservationOnReleasedAllocation,
			Severity:     consistencykernel.SeverityError,
			AllocationID: allocationID,
			RunID:        runID,
			NodeID:       nodeID,
			Status:       allocationLifecycle,
			Detail:       "active reservation remains after allocation release completed",
		})
	}
	return out, false, rows.Err()
}

func loadActiveLeaseIssues(ctx context.Context, q queryer, now time.Time, limit int) ([]consistencykernel.Issue, bool, error) {
	rows, err := q.Query(ctx, `
		SELECT el.lease_id, a.allocation_id, a.node_id, a.lifecycle_state, a.run_id
		FROM execution_leases el
		JOIN allocations a ON a.allocation_id = el.allocation_id
		WHERE el.revoked = FALSE
		  AND el.expires_at > $2
		  AND a.lifecycle_state = ANY($1::text[])
		ORDER BY el.created_at ASC, el.allocation_id ASC
		LIMIT $3
	`, terminalAllocationLifecycleStates(), now.UTC(), limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("query active lease consistency: %w", err)
	}
	defer rows.Close()
	return scanDependentIssues(rows, dependentResourceLease, limit)
}

func loadActiveTunnelIssues(ctx context.Context, q queryer, _ time.Time, limit int) ([]consistencykernel.Issue, bool, error) {
	rows, err := q.Query(ctx, `
		SELECT ts.session_id, a.allocation_id, a.node_id, a.lifecycle_state, a.run_id
		FROM tunnel_sessions ts
		JOIN allocations a ON a.allocation_id = ts.allocation_id
		WHERE ts.revoked = FALSE
		  AND ts.status IN (
			'TUNNEL_SESSION_STATUS_PENDING',
			'TUNNEL_SESSION_STATUS_RUNNING',
			'TUNNEL_SESSION_STATUS_DEGRADED'
		  )
		  AND a.lifecycle_state = ANY($1::text[])
		ORDER BY ts.created_at ASC, ts.allocation_id ASC
		LIMIT $2
	`, terminalAllocationLifecycleStates(), limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("query active tunnel consistency: %w", err)
	}
	defer rows.Close()
	return scanDependentIssues(rows, dependentResourceTunnel, limit)
}

func scanDependentIssues(rows pgx.Rows, resource dependentResource, limit int) ([]consistencykernel.Issue, bool, error) {
	var out []consistencykernel.Issue
	for rows.Next() {
		if len(out) == limit {
			return out, true, nil
		}
		var dependentID, allocationID, nodeID, status, runID string
		if err := rows.Scan(&dependentID, &allocationID, &nodeID, &status, &runID); err != nil {
			return nil, false, err
		}
		issue := consistencykernel.Issue{
			Severity:     consistencykernel.SeverityError,
			AllocationID: allocationID,
			RunID:        runID,
			NodeID:       nodeID,
			DependentID:  dependentID,
			Status:       status,
		}
		issue.Code = dependentIssueCode(resource)
		issue.Detail = string(resource) + " remains after allocation ended"
		out = append(out, issue)
	}
	return out, false, rows.Err()
}

func dependentIssueCode(resource dependentResource) consistencykernel.IssueCode {
	switch resource {
	case dependentResourceLease:
		return consistencykernel.IssueActiveLeaseOnEndedAllocation
	case dependentResourceTunnel:
		return consistencykernel.IssueActiveTunnelOnEndedAllocation
	default:
		return consistencykernel.IssueCode(resource + "_on_ended_allocation")
	}
}

func terminalAllocationLifecycleStates() []string {
	return []string{
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(),
	}
}
