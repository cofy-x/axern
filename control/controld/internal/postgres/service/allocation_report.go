package pgservice

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type serviceStatusTransition struct {
	allocation       *allocationRecord
	currentStatus    commonv1.AllocationStatus
	currentReady     bool
	nextStatus       commonv1.AllocationStatus
	nextReady        bool
	message          string
	readinessMessage string
}

func (s *PGStore) BatchReportAllocationStatus(ctx context.Context, nodeID string, observations []*nodev1.AllocationStatusObservation, now time.Time) (servicekernel.AllocationStatusBatchResult, error) {
	totalStarted := time.Now()
	allocationIDs := allocationIDsFromObservations(observations)
	var result servicekernel.AllocationStatusBatchResult
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		stageStarted := time.Now()
		lockedServices, err := lockServicesForStatusBatch(ctx, tx, allocationIDs)
		recordServiceStatusBatchStage(ctx, serviceStatusBatchStageLockServices, stageStarted, err)
		if err != nil {
			return err
		}
		stageStarted = time.Now()
		allocations, err := s.allocationRecordsForStatusBatch(ctx, tx, allocationIDs)
		recordServiceStatusBatchStage(ctx, serviceStatusBatchStageLockAllocations, stageStarted, err)
		if err != nil {
			return err
		}
		stageStarted = time.Now()
		transitions, err := s.updateServiceAllocationStatuses(ctx, tx, nodeID, observations, lockedServices, allocations, now)
		recordServiceStatusBatchStage(ctx, serviceStatusBatchStageUpdateAllocations, stageStarted, err)
		if err != nil {
			return err
		}
		byService := make(map[string][]*serviceStatusTransition)
		for _, transition := range transitions {
			byService[transition.allocation.OwnerID] = append(byService[transition.allocation.OwnerID], transition)
		}
		serviceIDs := make([]string, 0, len(byService))
		for serviceID := range byService {
			serviceIDs = append(serviceIDs, serviceID)
		}
		sort.Strings(serviceIDs)
		stageStarted = time.Now()
		for _, serviceID := range serviceIDs {
			current := lockedServices[serviceID]
			if current == nil {
				continue
			}
			serviceReports, needsReconcile, err := s.projectServiceStatusBatch(ctx, tx, current, byService[serviceID], now)
			if err != nil {
				recordServiceStatusBatchStage(ctx, serviceStatusBatchStageProjectServices, stageStarted, err)
				return err
			}
			result.Reports = append(result.Reports, serviceReports...)
			if needsReconcile {
				result.ReconcileServiceIDs = append(result.ReconcileServiceIDs, serviceID)
			}
		}
		recordServiceStatusBatchStage(ctx, serviceStatusBatchStageProjectServices, stageStarted, nil)
		return nil
	})
	recordServiceStatusBatchStage(ctx, serviceStatusBatchStageTotal, totalStarted, err)
	return result, err
}

func (s *PGStore) ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error {
	nodeID := strings.TrimSpace(snapshot.NodeID)
	if nodeID == "" {
		return nil
	}
	expected, err := s.activeServiceInventoryExpectations(ctx, nodeID)
	if err != nil {
		return err
	}
	missing := allocationkernel.MissingFromNodeInventory(snapshot, expected)
	observations := make([]*nodev1.AllocationStatusObservation, 0, len(missing))
	for _, alloc := range missing {
		observations = append(observations, &nodev1.AllocationStatusObservation{
			AllocationID:  alloc.AllocationID,
			Attempt:       alloc.Attempt,
			Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			Message:       allocationkernel.MissingFromNodeInventoryMessage,
			ObservedAt:    timestamppb.New(now.UTC()),
			ExitCodeKnown: false,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	_, err = s.BatchReportAllocationStatus(ctx, nodeID, observations, now)
	return err
}

func (s *PGStore) ReconcileNodeUnavailable(ctx context.Context, nodeID string, now time.Time) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}
	expected, err := s.unavailableServiceAllocations(ctx, nodeID)
	if err != nil {
		return err
	}
	observations := make([]*nodev1.AllocationStatusObservation, 0, len(expected))
	for _, alloc := range expected {
		observations = append(observations, &nodev1.AllocationStatusObservation{
			AllocationID:  alloc.AllocationID,
			Attempt:       alloc.Attempt,
			Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			Message:       allocationkernel.NodeUnavailableMessage,
			ObservedAt:    timestamppb.New(now.UTC()),
			ExitCodeKnown: false,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	_, err = s.BatchReportAllocationStatus(ctx, nodeID, observations, now)
	return err
}

func lockServicesForStatusBatch(ctx context.Context, tx pgx.Tx, allocationIDs []string) (map[string]*servicev1.Service, error) {
	if len(allocationIDs) == 0 {
		return map[string]*servicev1.Service{}, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT owner_id
		FROM allocations
		WHERE owner_type = $1 AND allocation_id = ANY($2::text[])
		ORDER BY owner_id
	`, allocationOwnerService, allocationIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve services for allocation status batch: %w", err)
	}
	serviceIDs := make([]string, 0)
	for rows.Next() {
		var serviceID string
		if err := rows.Scan(&serviceID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan service for allocation status batch: %w", err)
		}
		serviceIDs = append(serviceIDs, serviceID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate services for allocation status batch: %w", err)
	}
	rows.Close()
	if len(serviceIDs) == 0 {
		return map[string]*servicev1.Service{}, nil
	}
	rows, err = tx.Query(ctx, serviceSelectSQL()+` WHERE service_id = ANY($1::text[]) ORDER BY service_id FOR UPDATE`, serviceIDs)
	if err != nil {
		return nil, fmt.Errorf("lock services for allocation status batch: %w", err)
	}
	defer rows.Close()
	services := make(map[string]*servicev1.Service, len(serviceIDs))
	for rows.Next() {
		service, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		services[service.GetID()] = service
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked services for allocation status batch: %w", err)
	}
	return services, nil
}

func (s *PGStore) updateServiceAllocationStatuses(ctx context.Context, tx pgx.Tx, nodeID string, observations []*nodev1.AllocationStatusObservation, lockedServices map[string]*servicev1.Service, allocations map[string]*allocationRecord, now time.Time) ([]*serviceStatusTransition, error) {
	byID := make(map[string]*nodev1.AllocationStatusObservation, len(observations))
	allocationIDs := make([]string, 0, len(observations))
	for _, observation := range observations {
		allocationID := strings.TrimSpace(observation.GetAllocationID())
		if allocationID == "" {
			continue
		}
		byID[allocationID] = observation
		allocationIDs = append(allocationIDs, allocationID)
	}
	sort.Strings(allocationIDs)
	transitions := make([]*serviceStatusTransition, 0, len(allocationIDs))
	endedAllocationIDs := make([]string, 0)
	for _, allocationID := range allocationIDs {
		observation := byID[allocationID]
		alloc := allocations[allocationID]
		if alloc == nil {
			continue
		}
		if alloc.OwnerType != allocationOwnerService || lockedServices[alloc.OwnerID] == nil {
			continue
		}
		if !allocationkernel.AcceptsObservation(alloc.Status, alloc.Attempt, alloc.NodeID, nodeID, observation) {
			continue
		}
		nextStatus := observation.GetStatus()
		nextReady := observation.GetReady() && nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING
		readinessMessage := strings.TrimSpace(observation.GetReadinessMessage())
		if nextStatus != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
			readinessMessage = ""
		}
		message := strings.TrimSpace(observation.GetMessage())
		conditions := &capabilityv1.CapabilityConditionSet{Conditions: observation.GetCapabilityConditions()}
		conditionsJSON, err := marshalProtoJSON(conditions)
		if err != nil {
			return nil, err
		}
		if serviceAllocationObservationMatches(alloc, observation, nextReady, readinessMessage, message, conditions) {
			continue
		}
		nodeActiveAt := allocationkernel.NodeActiveObservationTime(observation, now)
		if _, err := tx.Exec(ctx, `
			UPDATE allocations
			SET status = $2, ready = $3, readiness_message = $4, exit_code = $5, exit_code_known = $6, message = $7, capability_conditions = $14::jsonb,
				version = version + 1, updated_at = $8,
				node_active_at = CASE
					WHEN node_active_at IS NULL AND $2 IN ($11, $12) THEN $13
					ELSE node_active_at
				END
			WHERE allocation_id = $1 AND attempt = $9 AND owner_type = $10
		`, alloc.AllocationID, nextStatus.String(), nextReady, readinessMessage, observation.GetExitCode(), observation.GetExitCodeKnown(), message, now.UTC(), observation.GetAttempt(), allocationOwnerService, commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String(), nodeActiveAt, conditionsJSON); err != nil {
			return nil, fmt.Errorf("update service allocation status: %w", err)
		}
		transitions = append(transitions, &serviceStatusTransition{
			allocation:       alloc,
			currentStatus:    alloc.Status,
			currentReady:     alloc.Ready,
			nextStatus:       nextStatus,
			nextReady:        nextReady,
			message:          message,
			readinessMessage: readinessMessage,
		})
		if allocationkernel.IsEnded(nextStatus) {
			endedAllocationIDs = append(endedAllocationIDs, alloc.AllocationID)
		}
	}
	if len(endedAllocationIDs) == 0 {
		return transitions, nil
	}
	if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
		AllocationIDs: endedAllocationIDs,
		Reason:        "service allocation ended",
		ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED,
		Now:           now,
	}); err != nil {
		return nil, err
	}
	if err := s.revokeAllocationLeases(ctx, tx, endedAllocationIDs); err != nil {
		return nil, err
	}
	if err := pgreservation.ReleaseAllocations(ctx, tx, endedAllocationIDs, now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM allocation_reconcile_queue WHERE allocation_id = ANY($1::text[])`, endedAllocationIDs); err != nil {
		return nil, fmt.Errorf("delete ended service allocation reconcile items: %w", err)
	}
	return transitions, nil
}

func serviceAllocationObservationMatches(alloc *allocationRecord, observation *nodev1.AllocationStatusObservation, ready bool, readinessMessage, message string, conditions *capabilityv1.CapabilityConditionSet) bool {
	return alloc != nil && observation != nil &&
		alloc.Status == observation.GetStatus() &&
		alloc.Ready == ready &&
		strings.TrimSpace(alloc.ReadinessMessage) == readinessMessage &&
		alloc.ExitCode == observation.GetExitCode() &&
		alloc.ExitCodeKnown == observation.GetExitCodeKnown() &&
		strings.TrimSpace(alloc.Message) == message && proto.Equal(alloc.CapabilityConditions, conditions)
}

func (s *PGStore) projectServiceStatusBatch(ctx context.Context, tx pgx.Tx, current *servicev1.Service, transitions []*serviceStatusTransition, now time.Time) ([]*servicekernel.AllocationStatusReport, bool, error) {
	next := servicekernel.CloneService(current)
	for _, transition := range transitions {
		if !allocationkernel.IsEnded(transition.nextStatus) {
			continue
		}
		next.AllocationIds = removeAllocationID(next.GetAllocationIds(), transition.allocation.AllocationID)
		if transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED || transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
			next.UnhealthyReplicas++
		}
	}
	allocations, err := s.listAllocationRecordsTx(ctx, tx, next.GetID(), next.GetAllocationIds())
	if err != nil {
		return nil, false, err
	}
	applyObservedHealth(next, deriveObservedHealth(next, allocationStatusesFromRecords(allocations)))
	applyObservedStatusBatchMessage(next, transitions)
	applyServiceDiagnostics(next)
	result := &serviceObservationResult{
		service: next,
		rollout: servicekernel.BuildRolloutStatus(next, allocations),
	}
	applyRolloutReconciliation(next, result.rollout)
	next.Version++
	next.UpdatedAt = timestamppb.New(now)
	if err := s.persistService(ctx, tx, next, now); err != nil {
		return nil, false, err
	}
	if err := s.recordServiceObservationBatchEvents(ctx, tx, current, result, transitions, now); err != nil {
		return nil, false, err
	}
	return buildAllocationStatusReports(current, next, transitions, now), serviceStatusBatchNeedsReconcile(result, transitions), nil
}

func serviceStatusBatchNeedsReconcile(result *serviceObservationResult, transitions []*serviceStatusTransition) bool {
	for _, transition := range transitions {
		if transition != nil && allocationkernel.IsEnded(transition.nextStatus) {
			return true
		}
	}
	if result == nil || result.rollout == nil || !result.rollout.GetInProgress() {
		return false
	}
	switch result.rollout.GetPhase() {
	case servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_ADMITTING_REPLACEMENT,
		servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_DRAINING_OUTDATED:
		return true
	default:
		return false
	}
}

func allocationIDsFromObservations(observations []*nodev1.AllocationStatusObservation) []string {
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
