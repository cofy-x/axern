package pgrun

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) BatchReportAllocationStatus(ctx context.Context, nodeID string, observations []*nodev1.AllocationStatusObservation, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		allocations, err := lockRunStatusAllocations(ctx, tx, allocationIDsFromRunObservations(observations))
		if err != nil {
			return err
		}
		for _, obs := range observations {
			alloc := allocations[strings.TrimSpace(obs.GetAllocationID())]
			if alloc == nil {
				continue
			}
			if !allocationkernel.AcceptsObservation(alloc.status, alloc.attempt, alloc.nodeID, nodeID, obs) {
				continue
			}
			nextStatus := obs.GetStatus()
			message := strings.TrimSpace(obs.GetMessage())
			if runAllocationObservationMatches(alloc, obs, message) {
				continue
			}
			nodeActiveAt := allocationkernel.NodeActiveObservationTime(obs, now)
			if _, err := tx.Exec(ctx, `
			UPDATE allocations
			SET status = $2, exit_code = $3, exit_code_known = $4, message = $5,
				version = version + 1, updated_at = $6,
				node_active_at = CASE
					WHEN node_active_at IS NULL AND $2 IN ($8, $9) THEN $10
					ELSE node_active_at
				END
			WHERE allocation_id = $1 AND attempt = $7
			`, alloc.allocationID, nextStatus.String(), obs.GetExitCode(), obs.GetExitCodeKnown(), message, now.UTC(), obs.GetAttempt(), commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String(), nodeActiveAt); err != nil {
				return fmt.Errorf("update allocation status: %w", err)
			}
			runStatus := allocationkernel.RunStatusFromAllocation(nextStatus, obs.GetExitCode())
			if _, err := tx.Exec(ctx, `
			UPDATE runs
			SET status = $2, exit_code = $3, exit_code_known = $4, message = $5,
				version = version + 1, updated_at = $6
			WHERE allocation_id = $1 AND attempt = $7 AND status NOT IN ($8, $9, $10)
			`, alloc.allocationID, runStatus.String(), obs.GetExitCode(), obs.GetExitCodeKnown(), message, now.UTC(), obs.GetAttempt(), runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(), runv1.RunStatus_RUN_STATUS_FAILED.String(), runv1.RunStatus_RUN_STATUS_CANCELLED.String()); err != nil {
				return fmt.Errorf("update run status: %w", err)
			}
			if runkernel.IsTerminal(runStatus) {
				if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
					AllocationIDs: []string{alloc.allocationID},
					Reason:        "run allocation terminated",
					ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED,
					Now:           now,
				}); err != nil {
					return err
				}
				if err := s.revokeAllocationLeases(ctx, tx, alloc.allocationID, now); err != nil {
					return err
				}
				if err := pgreservation.ReleaseAllocation(ctx, tx, alloc.allocationID, now); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `DELETE FROM allocation_reconcile_queue WHERE allocation_id = $1`, alloc.allocationID); err != nil {
					return fmt.Errorf("delete terminal allocation reconcile item: %w", err)
				}
			}
		}
		return nil
	})
}

type runStatusAllocation struct {
	allocationID  string
	nodeID        string
	attempt       int64
	status        commonv1.AllocationStatus
	exitCode      int32
	exitCodeKnown bool
	message       string
}

func runAllocationObservationMatches(allocation *runStatusAllocation, observation *nodev1.AllocationStatusObservation, message string) bool {
	return allocation != nil && observation != nil &&
		allocation.status == observation.GetStatus() &&
		allocation.exitCode == observation.GetExitCode() &&
		allocation.exitCodeKnown == observation.GetExitCodeKnown() &&
		strings.TrimSpace(allocation.message) == message
}

func lockRunStatusAllocations(ctx context.Context, tx pgx.Tx, allocationIDs []string) (map[string]*runStatusAllocation, error) {
	allocations := make(map[string]*runStatusAllocation, len(allocationIDs))
	if len(allocationIDs) == 0 {
		return allocations, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT allocation_id, node_id, attempt, status, exit_code, exit_code_known, message
		FROM allocations
		WHERE owner_type = $1 AND allocation_id = ANY($2::text[])
		ORDER BY allocation_id
		FOR UPDATE
	`, allocationOwnerRun, allocationIDs)
	if err != nil {
		return nil, fmt.Errorf("lock run allocations for status batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		allocation := &runStatusAllocation{}
		var statusText string
		if err := rows.Scan(&allocation.allocationID, &allocation.nodeID, &allocation.attempt, &statusText, &allocation.exitCode, &allocation.exitCodeKnown, &allocation.message); err != nil {
			return nil, fmt.Errorf("scan run allocation for status batch: %w", err)
		}
		allocation.status = parseAllocationStatus(statusText)
		allocations[allocation.allocationID] = allocation
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run allocations for status batch: %w", err)
	}
	return allocations, nil
}

func allocationIDsFromRunObservations(observations []*nodev1.AllocationStatusObservation) []string {
	ids := make([]string, 0, len(observations))
	seen := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		allocationID := strings.TrimSpace(observation.GetAllocationID())
		if allocationID == "" {
			continue
		}
		if _, ok := seen[allocationID]; ok {
			continue
		}
		seen[allocationID] = struct{}{}
		ids = append(ids, allocationID)
	}
	sort.Strings(ids)
	return ids
}

func (s *Store) ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error {
	nodeID := strings.TrimSpace(snapshot.NodeID)
	if nodeID == "" {
		return nil
	}
	expected, err := s.activeRunInventoryExpectations(ctx, nodeID)
	if err != nil {
		return err
	}
	for _, alloc := range allocationkernel.MissingFromNodeInventory(snapshot, expected) {
		if err := s.BatchReportAllocationStatus(ctx, nodeID, []*nodev1.AllocationStatusObservation{{
			AllocationID:  alloc.AllocationID,
			Attempt:       alloc.Attempt,
			Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			Message:       allocationkernel.MissingFromNodeInventoryMessage,
			ObservedAt:    timestamppb.New(now.UTC()),
			ExitCodeKnown: false,
		}}, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReconcileNodeUnavailable(ctx context.Context, nodeID string, now time.Time) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}
	expected, err := s.unavailableRunAllocations(ctx, nodeID)
	if err != nil {
		return err
	}
	for _, alloc := range expected {
		if err := s.BatchReportAllocationStatus(ctx, nodeID, []*nodev1.AllocationStatusObservation{{
			AllocationID:  alloc.AllocationID,
			Attempt:       alloc.Attempt,
			Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			Message:       allocationkernel.NodeUnavailableMessage,
			ObservedAt:    timestamppb.New(now.UTC()),
			ExitCodeKnown: false,
		}}, now); err != nil {
			return err
		}
	}
	return nil
}
