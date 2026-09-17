package pgrun

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) BatchReportAllocationLifecycle(ctx context.Context, nodeID string, observations []*nodev1.AllocationLifecycleObservation, now time.Time) error {
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		allocations, err := lockReportedAllocations(ctx, tx, allocationIDsFromRunObservations(observations))
		if err != nil {
			return err
		}
		for _, obs := range observations {
			alloc := allocations[strings.TrimSpace(obs.GetAllocationID())]
			if alloc == nil {
				continue
			}
			if err := allocationkernel.ValidateObservationBinding(alloc.nodeID, nodeID); err != nil {
				// Never acknowledge a report from the wrong node: doing so would
				// let that node retire its durable terminal outbox without the
				// authoritative binding accepting the observation.
				return fmt.Errorf("reject allocation %q lifecycle observation: %w", alloc.allocationID, err)
			}
			if !allocationkernel.AcceptsObservation(alloc.lifecycleState, alloc.nodeID, nodeID, obs) {
				continue
			}
			observedState := obs.GetState()
			message := strings.TrimSpace(obs.GetMessage())
			diagnosticCode := obs.GetDiagnosticCode()
			runStatus := allocationkernel.RunStatusFromObservation(observedState, obs.ExitCode, diagnosticCode)
			persistedState := observedState
			if observedState == commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED {
				persistedState = commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING
			}
			if runAllocationObservationMatches(alloc, persistedState, runStatus, obs, diagnosticCode, message) {
				continue
			}
			nodeActiveAt := allocationkernel.NodeActiveObservationTime(obs, now)
			if _, err := tx.Exec(ctx, `
			UPDATE allocations
			SET lifecycle_state = $2, updated_at = $3,
				node_active_at = CASE
					WHEN node_active_at IS NULL AND $2 IN ($4, $5) THEN $6
					ELSE node_active_at
				END
			WHERE allocation_id = $1
			`, alloc.allocationID, persistedState.String(), now.UTC(), commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING.String(), commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String(), nodeActiveAt); err != nil {
				return fmt.Errorf("update allocation lifecycle: %w", err)
			}
			if _, err := tx.Exec(ctx, `
			UPDATE runs
			SET status = $2, exit_code = $3, diagnostic_code = $4, message = $5,
				version = version + 1, updated_at = $6
			WHERE run_id = $1 AND status NOT IN ($7, $8, $9)
			`, alloc.runID, runStatus.String(), obs.ExitCode, diagnosticCode.String(), message, now.UTC(), runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(), runv1.RunStatus_RUN_STATUS_FAILED.String(), runv1.RunStatus_RUN_STATUS_CANCELLED.String()); err != nil {
				return fmt.Errorf("update run status: %w", err)
			}
			if runkernel.IsTerminal(runStatus) {
				if err := deleteRunSecretReferences(ctx, tx, alloc.runID); err != nil {
					return err
				}
				if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
					AllocationIDs: []string{alloc.allocationID},
					Reason:        "run allocation terminated",
					ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED,
					Now:           now,
				}); err != nil {
					return err
				}
				if err := pgallocation.RevokeAccessGrants(ctx, tx, alloc.allocationID); err != nil {
					return err
				}
				if err := pgallocation.ScheduleReconcile(ctx, tx, allocationkernel.ScheduleReconcileRequest{
					AllocationID: alloc.allocationID,
					Intent:       allocationkernel.ReconcileIntentEnsureAbsent,
					NextRunAt:    now,
				}, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err == nil && hasTerminalAllocationObservation(observations) {
		s.signalReconcileWork()
	}
	return err
}

func hasTerminalAllocationObservation(observations []*nodev1.AllocationLifecycleObservation) bool {
	for _, observation := range observations {
		if observation.GetState() == commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED {
			return true
		}
	}
	return false
}

type reportedAllocation struct {
	allocationID   string
	runID          string
	nodeID         string
	lifecycleState commonv1.AllocationLifecycleState
	runStatus      runv1.RunStatus
	exitCode       *int32
	diagnosticCode commonv1.WorkloadDiagnosticCode
	message        string
}

func runAllocationObservationMatches(allocation *reportedAllocation, lifecycleState commonv1.AllocationLifecycleState, runStatus runv1.RunStatus, observation *nodev1.AllocationLifecycleObservation, diagnosticCode commonv1.WorkloadDiagnosticCode, message string) bool {
	return allocation != nil && observation != nil &&
		allocation.lifecycleState == lifecycleState && allocation.runStatus == runStatus &&
		equalOptionalInt32(allocation.exitCode, observation.ExitCode) &&
		allocation.diagnosticCode == diagnosticCode && strings.TrimSpace(allocation.message) == message
}

func lockReportedAllocations(ctx context.Context, tx pgx.Tx, allocationIDs []string) (map[string]*reportedAllocation, error) {
	allocations := make(map[string]*reportedAllocation, len(allocationIDs))
	if len(allocationIDs) == 0 {
		return allocations, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT a.allocation_id, a.run_id, a.node_id, a.lifecycle_state,
			r.status, r.exit_code, r.diagnostic_code, r.message
		FROM allocations a
		JOIN runs r ON r.run_id = a.run_id
		WHERE a.allocation_id = ANY($1::text[])
		ORDER BY a.allocation_id
		FOR UPDATE OF a, r
	`, allocationIDs)
	if err != nil {
		return nil, fmt.Errorf("lock run allocations for lifecycle batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		allocation := &reportedAllocation{}
		var lifecycleStateText, runStatusText string
		var diagnosticCodeText string
		var exitCode sql.NullInt32
		if err := rows.Scan(&allocation.allocationID, &allocation.runID, &allocation.nodeID, &lifecycleStateText, &runStatusText, &exitCode, &diagnosticCodeText, &allocation.message); err != nil {
			return nil, fmt.Errorf("scan run allocation for lifecycle batch: %w", err)
		}
		if exitCode.Valid {
			value := exitCode.Int32
			allocation.exitCode = &value
		}
		allocation.lifecycleState = allocationkernel.ParseLifecycleState(lifecycleStateText)
		allocation.runStatus = parseRunStatus(runStatusText)
		allocation.diagnosticCode = parseWorkloadDiagnosticCode(diagnosticCodeText)
		allocations[allocation.allocationID] = allocation
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run allocations for lifecycle batch: %w", err)
	}
	return allocations, nil
}

func allocationIDsFromRunObservations(observations []*nodev1.AllocationLifecycleObservation) []string {
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
		if err := s.BatchReportAllocationLifecycle(ctx, nodeID, []*nodev1.AllocationLifecycleObservation{{
			AllocationID:   alloc.AllocationID,
			State:          commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED,
			Message:        allocationkernel.MissingFromNodeInventoryMessage,
			DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR,
			ObservedAt:     timestamppb.New(now.UTC()),
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
		if err := s.BatchReportAllocationLifecycle(ctx, nodeID, []*nodev1.AllocationLifecycleObservation{{
			AllocationID:   alloc.AllocationID,
			State:          commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED,
			Message:        allocationkernel.NodeUnavailableMessage,
			DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR,
			ObservedAt:     timestamppb.New(now.UTC()),
		}}, now); err != nil {
			return err
		}
	}
	return nil
}

func equalOptionalInt32(left, right *int32) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
