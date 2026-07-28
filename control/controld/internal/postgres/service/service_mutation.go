package pgservice

import (
	"context"
	"strings"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *PGStore) UpdateDeletionStatus(ctx context.Context, serviceID string, deletion *servicev1.ServiceDeletionStatus, now time.Time) (*servicev1.Service, error) {
	var service *servicev1.Service
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, strings.TrimSpace(serviceID)))
		if err != nil {
			return err
		}
		if current.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETING && current.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
			return grpcstatus.Errorf(codes.FailedPrecondition, "service %q is not deleting", current.GetID())
		}
		next := servicekernel.ApplyDeletionProgress(current, deletion, now)
		if err := s.persistService(ctx, tx, next, now); err != nil {
			return err
		}
		service = next
		return nil
	})
	return service, err
}

func (s *PGStore) Update(ctx context.Context, req *servicev1.UpdateServiceRequest, now time.Time) (*servicev1.Service, error) {
	var updated *servicev1.Service
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, strings.TrimSpace(req.GetServiceID())))
		if err == pgx.ErrNoRows {
			updated = nil
			return nil
		}
		if err != nil {
			return err
		}
		next, err := servicekernel.ApplyUpdate(current, req, now)
		if err != nil || next == nil {
			return err
		}
		if err := s.persistService(ctx, tx, next, now); err != nil {
			return err
		}
		applyServiceDiagnostics(next)
		updated = next
		return nil
	})
	return updated, err
}

func (s *PGStore) Delete(ctx context.Context, params servicekernel.DeleteParams, now time.Time) (*servicev1.Service, bool, error) {
	var deleted *servicev1.Service
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, strings.TrimSpace(params.ServiceID)))
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if current.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETING || current.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
			deleted = current
			return nil
		}
		if params.ExpectedVersion > 0 && current.GetVersion() != params.ExpectedVersion {
			return grpcstatus.Errorf(codes.Aborted, "service %q version mismatch: got %d, want %d", current.GetID(), current.GetVersion(), params.ExpectedVersion)
		}
		if params.RequireSuspended && current.GetReplicas() != 0 {
			return grpcstatus.Errorf(codes.FailedPrecondition, "service %q must be suspended before deletion", current.GetID())
		}
		deleted = servicekernel.MarkDeleted(current, params.VolumeDisposition, now)
		if err := s.persistService(ctx, tx, deleted, now); err != nil {
			return err
		}
		applyServiceDiagnostics(deleted)
		return s.revokeAllocationLeases(ctx, tx, deleted.GetAllocationIds())
	})
	return deleted, deleted != nil, err
}

func (s *PGStore) UpdateStatus(ctx context.Context, serviceID string, status servicev1.ServiceStatus, message string, now time.Time) (*servicev1.Service, error) {
	var service *servicev1.Service
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, strings.TrimSpace(serviceID)))
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		next, changed := servicekernel.ApplyStatusUpdate(current, status, message, now)
		if !changed {
			service = next
			return nil
		}
		if err := s.persistService(ctx, tx, next, now); err != nil {
			return err
		}
		applyServiceDiagnostics(next)
		service = next
		return nil
	})
	return service, err
}

func (s *PGStore) SyncObservedStatus(ctx context.Context, serviceID string, now time.Time) (*servicev1.Service, error) {
	var service *servicev1.Service
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, strings.TrimSpace(serviceID)))
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		next := servicekernel.CloneService(current)
		allocations, err := s.listAllocationRecordsTx(ctx, tx, next.GetID(), next.GetAllocationIds())
		if err != nil {
			return err
		}
		applyObservedHealth(next, deriveObservedHealth(next, allocationStatusesFromRecords(allocations)))
		applyRolloutReconciliation(next, servicekernel.BuildRolloutStatus(next, allocations))
		next.AutoscalingPolicy = servicekernel.CloneAutoscalingPolicy(current.GetAutoscalingPolicy())
		next.AutoscalingStatus = servicekernel.CloneAutoscalingStatus(current.GetAutoscalingStatus())
		if next.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_READY {
			next.Message = ""
		}
		next.Version++
		next.UpdatedAt = timestamppb.New(now)
		if err := s.persistService(ctx, tx, next, now); err != nil {
			return err
		}
		applyServiceDiagnostics(next)
		service = next
		return nil
	})
	return service, err
}

func (s *PGStore) UpdateAutoscalingStatus(ctx context.Context, serviceID string, autoscaling *servicev1.ServiceAutoscalingStatus, now time.Time) (*servicev1.Service, error) {
	var service *servicev1.Service
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, strings.TrimSpace(serviceID)))
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		next := servicekernel.CloneService(current)
		next.AutoscalingStatus = servicekernel.NormalizeAutoscalingStatus(autoscaling)
		next.Version++
		next.UpdatedAt = timestamppb.New(now)
		if err := s.persistService(ctx, tx, next, now); err != nil {
			return err
		}
		applyServiceDiagnostics(next)
		service = next
		return nil
	})
	return service, err
}
