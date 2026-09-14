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
		currentEnvironment, err := scanEnvironment(tx.QueryRow(ctx, environmentSelectSQL()+` WHERE environment_id = $1 FOR SHARE`, params.Environment.GetID()))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return grpcstatus.Errorf(codes.NotFound, "environment %q not found", params.Environment.GetID())
			}
			return err
		}
		namespace := environmentkernel.NormalizeNamespace(params.Namespace)
		if currentEnvironment.GetNamespace() != namespace {
			return grpcstatus.Errorf(codes.InvalidArgument, "run namespace %q does not own environment %q", namespace, currentEnvironment.GetID())
		}
		runID := "run-" + uuid.NewString()
		allocationID := "alloc-" + uuid.NewString()
		selected, err := s.reserveCandidate(ctx, tx, pgreservation.ReserveCandidateRequest{
			Namespace:     namespace,
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
		environmentSpecJSON, err := marshalProtoJSON(currentEnvironment.GetSpec())
		if err != nil {
			return err
		}
		resolvedEnvironmentSpecJSON, err := marshalProtoJSON(currentEnvironment.GetResolvedSpec())
		if err != nil {
			return err
		}
		labelsJSON, err := marshalJSONMap(params.Labels)
		if err != nil {
			return err
		}
		run = &runv1.Run{
			ID:                      runID,
			Namespace:               namespace,
			EnvironmentID:           params.Environment.GetID(),
			AllocationID:            allocationID,
			Status:                  runv1.RunStatus_RUN_STATUS_PLACED,
			Config:                  runkernel.CloneConfig(normalizedConfig),
			EnvironmentSpec:         cloneEnvironmentSpec(currentEnvironment.GetSpec()),
			ResolvedEnvironmentSpec: cloneResolvedEnvironmentSpec(currentEnvironment.GetResolvedSpec()),
			Labels:                  runkernel.CloneLabels(params.Labels),
			Version:                 1,
			CreatedAt:               timestamppb.New(now),
			UpdatedAt:               timestamppb.New(now),
		}
		alloc = &runkernel.AllocationRecord{
			AllocationID:           run.GetAllocationID(),
			NodeID:                 selected.Record.NodeID,
			NodeTarget:             selected.Record.NodeTarget,
			CapabilityRequirements: selected.CapabilityRequirements,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO runs (
				run_id, namespace, environment_id, status,
				config, environment_spec, resolved_environment_spec, labels, version, created_at, updated_at, message
			) VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8::jsonb, $9, $10, $11, '')
		`, run.GetID(), run.GetNamespace(), run.GetEnvironmentID(), run.GetStatus().String(), cfgJSON, environmentSpecJSON, resolvedEnvironmentSpecJSON, labelsJSON, run.GetVersion(), now.UTC(), now.UTC()); err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		if err := insertRunSecretReferences(ctx, tx, run); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO allocations (
				allocation_id, run_id, node_id, lifecycle_state, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6)
		`, alloc.AllocationID, run.GetID(), alloc.NodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND.String(), now.UTC(), now.UTC()); err != nil {
			return fmt.Errorf("insert allocation: %w", err)
		}
		if err := pgallocation.InsertCapabilityRequirements(ctx, tx, alloc.AllocationID, selected.CapabilityRequirements, now); err != nil {
			return err
		}
		res := normalizedConfig.GetResources().GetRequests()
		if err := pgreservation.InsertReservation(ctx, tx, pgreservation.Reservation{
			AllocationID: alloc.AllocationID,
			NodeID:       alloc.NodeID,
			Requests:     res,
			CreatedAt:    now,
		}); err != nil {
			return err
		}
		if err := pgallocation.ScheduleReconcile(ctx, tx, allocationkernel.ScheduleReconcileRequest{
			AllocationID: alloc.AllocationID,
			Intent:       allocationkernel.ReconcileIntentEnsurePresent,
			NextRunAt:    now,
		}, now); err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		s.signalReconcileWork()
	}
	return run, err
}
