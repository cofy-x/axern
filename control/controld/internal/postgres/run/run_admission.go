package pgrun

import (
	"context"
	"errors"
	"fmt"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) AdmitRun(ctx context.Context, params runkernel.AdmitRunParams, now time.Time) (*runv1.Run, error) {
	if len(params.Candidates) == 0 {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "no eligible node")
	}
	var (
		run   *runv1.Run
		alloc *runkernel.AllocationRecord
	)
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := scanEnvironment(tx.QueryRow(ctx, environmentSelectSQL()+` WHERE environment_id = $1`, params.Environment.GetID())); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return grpcstatus.Errorf(codes.NotFound, "environment %q not found", params.Environment.GetID())
			}
			return err
		}
		namespace := environmentkernel.NormalizeNamespace(params.Namespace)
		runID := "run-" + uuid.NewString()
		allocationID := "alloc-" + uuid.NewString()
		selected, err := s.reserveCandidate(ctx, tx, pgreservation.ReserveCandidateRequest{
			Namespace:     namespace,
			OwnerType:     allocationOwnerRun,
			OwnerID:       runID,
			EnvironmentID: params.Environment.GetID(),
			Candidates:    params.Candidates,
			Config:        params.Config,
			Now:           now,
		})
		if err != nil {
			return err
		}
		normalizedConfig := executionkernel.NormalizeConfig(params.Config)
		cfgJSON, err := marshalProtoJSON(normalizedConfig)
		if err != nil {
			return err
		}
		labelsJSON, err := marshalJSONMap(params.Labels)
		if err != nil {
			return err
		}
		run = &runv1.Run{
			ID:            runID,
			Namespace:     namespace,
			EnvironmentID: params.Environment.GetID(),
			AllocationID:  allocationID,
			Attempt:       1,
			Status:        runv1.RunStatus_RUN_STATUS_PLACED,
			Config:        runkernel.CloneConfig(normalizedConfig),
			Labels:        runkernel.CloneLabels(params.Labels),
			Version:       1,
			CreatedAt:     timestamppb.New(now),
			UpdatedAt:     timestamppb.New(now),
		}
		alloc = &runkernel.AllocationRecord{
			AllocationID: run.GetAllocationID(),
			NodeID:       selected.NodeID,
			NodeTarget:   selected.NodeTarget,
			Attempt:      run.GetAttempt(),
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO runs (
				run_id, namespace, environment_id, allocation_id, attempt, status,
				config, labels, version, created_at, updated_at, exit_code, exit_code_known, message
			) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11, 0, false, '')
		`, run.GetID(), run.GetNamespace(), run.GetEnvironmentID(), run.GetAllocationID(), run.GetAttempt(), run.GetStatus().String(), cfgJSON, labelsJSON, run.GetVersion(), now.UTC(), now.UTC()); err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO allocations (
				allocation_id, owner_type, owner_id, environment_id, node_id, attempt, status,
				config, version, created_at, updated_at, exit_code, exit_code_known, message
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, 1, $9, $10, 0, false, '')
		`, alloc.AllocationID, allocationOwnerRun, run.GetID(), params.Environment.GetID(), alloc.NodeID, alloc.Attempt, commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND.String(), cfgJSON, now.UTC(), now.UTC()); err != nil {
			return fmt.Errorf("insert allocation: %w", err)
		}
		res := normalizedConfig.GetResources().GetRequests()
		if err := pgreservation.InsertWorkloadReservation(ctx, tx, pgreservation.WorkloadReservation{
			AllocationID: alloc.AllocationID,
			Namespace:    run.GetNamespace(),
			OwnerType:    allocationOwnerRun,
			OwnerID:      run.GetID(),
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
		return nil
	})
	return run, err
}
