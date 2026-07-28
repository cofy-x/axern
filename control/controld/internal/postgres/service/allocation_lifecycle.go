package pgservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *PGStore) AdmitAllocation(ctx context.Context, serviceID string, config *commonv1.ExecutionConfig, candidates []*placementkernel.Candidate, now time.Time) (*servicev1.Service, *servicekernel.AllocationRecord, error) {
	var (
		service *servicev1.Service
		alloc   *servicekernel.AllocationRecord
	)
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, strings.TrimSpace(serviceID)))
		if err != nil {
			return err
		}
		if current.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETING || current.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
			return grpcstatus.Errorf(codes.FailedPrecondition, "service %q is deleting or deleted", current.GetID())
		}
		selected, err := s.reservations.ReserveCandidate(ctx, tx, pgreservation.ReserveCandidateRequest{
			Namespace:     current.GetNamespace(),
			OwnerType:     allocationOwnerService,
			OwnerID:       current.GetID(),
			EnvironmentID: current.GetEnvironmentID(),
			Candidates:    candidates,
			Config:        config,
			Now:           now,
		})
		if err != nil {
			return err
		}
		normalizedConfig := executionkernel.NormalizeConfig(config)
		configJSON, err := marshalProtoJSON(normalizedConfig)
		if err != nil {
			return err
		}
		readinessProbeJSON, err := marshalProtoJSON(servicekernel.CloneReadinessProbe(current.GetReadinessProbe()))
		if err != nil {
			return err
		}
		livenessProbeJSON, err := marshalProtoJSON(servicekernel.CloneLivenessProbe(current.GetLivenessProbe()))
		if err != nil {
			return err
		}
		alloc = &servicekernel.AllocationRecord{
			AllocationID:   "alloc-" + uuid.NewString(),
			ServiceID:      current.GetID(),
			NodeID:         selected.NodeID,
			NodeTarget:     selected.NodeTarget,
			Attempt:        1,
			ReadinessProbe: servicekernel.CloneReadinessProbe(current.GetReadinessProbe()),
			LivenessProbe:  servicekernel.CloneLivenessProbe(current.GetLivenessProbe()),
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO allocations (
				allocation_id, owner_type, owner_id, environment_id, node_id, attempt, status, ready, readiness_message, readiness_probe, liveness_probe,
				config, version, created_at, updated_at, exit_code, exit_code_known, message
			) VALUES ($1, $2, $3, $4, $5, $6, $7, false, '', $8::jsonb, $9::jsonb, $10::jsonb, 1, $11, $12, 0, false, '')
		`, alloc.AllocationID, allocationOwnerService, current.GetID(), current.GetEnvironmentID(), alloc.NodeID, alloc.Attempt, commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND.String(), readinessProbeJSON, livenessProbeJSON, configJSON, now.UTC(), now.UTC()); err != nil {
			return fmt.Errorf("insert service allocation: %w", err)
		}
		res := normalizedConfig.GetResources().GetRequests()
		if err := pgreservation.InsertWorkloadReservation(ctx, tx, pgreservation.WorkloadReservation{
			AllocationID: alloc.AllocationID,
			Namespace:    current.GetNamespace(),
			OwnerType:    allocationOwnerService,
			OwnerID:      current.GetID(),
			NodeID:       alloc.NodeID,
			Requests:     res,
			CreatedAt:    now,
		}); err != nil {
			return err
		}
		if err := pgallocation.ScheduleReconcile(ctx, tx, allocationkernel.ScheduleReconcileRequest{
			AllocationID: alloc.AllocationID,
			Reason:       allocationkernel.ReconcileReasonCreate,
			NextRunAt:    now,
		}, now); err != nil {
			return err
		}
		next := servicekernel.CloneService(current)
		next.AllocationIds = append(append([]string(nil), current.GetAllocationIds()...), alloc.AllocationID)
		next.Status = computeServiceStatus(next)
		next.Message = ""
		next.Version++
		next.UpdatedAt = timestamppb.New(now)
		if err := s.persistService(ctx, tx, next, now); err != nil {
			return err
		}
		service = next
		return nil
	})
	return service, alloc, err
}

func (s *PGStore) BeginAllocationRelease(ctx context.Context, serviceID, allocationID string, now time.Time) (*servicev1.Service, *servicekernel.AllocationRecord, error) {
	var (
		service *servicev1.Service
		alloc   *servicekernel.AllocationRecord
	)
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, strings.TrimSpace(serviceID)))
		if err != nil {
			return err
		}
		alloc, err = s.serviceAllocation(ctx, tx, current.GetID(), allocationID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE allocations
			SET status = $2, version = version + 1, updated_at = $3
			WHERE allocation_id = $1 AND owner_type = $4 AND owner_id = $5
		`, alloc.AllocationID, commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING.String(), now.UTC(), allocationOwnerService, current.GetID()); err != nil {
			return fmt.Errorf("mark service allocation releasing: %w", err)
		}
		if err := pgallocation.ScheduleReconcile(ctx, tx, allocationkernel.ScheduleDeleteRequest(alloc.AllocationID, now), now); err != nil {
			return err
		}
		if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
			AllocationIDs: []string{alloc.AllocationID},
			Reason:        "service allocation releasing",
			ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED,
			Now:           now,
		}); err != nil {
			return err
		}
		next := servicekernel.CloneService(current)
		next.AllocationIds = removeAllocationID(next.GetAllocationIds(), alloc.AllocationID)
		next.Status = computeServiceStatus(next)
		next.Version++
		next.UpdatedAt = timestamppb.New(now)
		if err := s.persistService(ctx, tx, next, now); err != nil {
			return err
		}
		service = next
		return nil
	})
	return service, alloc, err
}
