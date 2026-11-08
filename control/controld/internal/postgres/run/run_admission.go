package pgrun

import (
	"context"
	"errors"
	"fmt"
	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	workspacekernel "github.com/cofy-x/axern/control/controld/internal/kernel/workspace"
	pgaccess "github.com/cofy-x/axern/control/controld/internal/postgres/access"
	pgworkspace "github.com/cofy-x/axern/control/controld/internal/postgres/workspace"
	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	resourceadmission "github.com/cofy-x/axern/control/controld/internal/postgres/resourceadmission"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) AdmitRun(ctx context.Context, params runkernel.AdmitRunParams, now time.Time) (*runv1.Run, error) {
	if params.WorkspaceID != "" {
		if err := executionkernel.ValidateWorkspaceConfig(params.Config); err != nil {
			return nil, err
		}
	}
	if len(params.Candidates) == 0 {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "no eligible node")
	}
	var (
		run   *runv1.Run
		alloc *runkernel.AllocationRecord
	)
	started := time.Now()
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
		candidates := params.Candidates
		if id := strings.TrimSpace(params.WorkspaceID); id != "" {
			var nodeID, ownerNamespace string
			err := tx.QueryRow(ctx, `SELECT node_id,namespace FROM workspaces WHERE workspace_id=$1`, id).Scan(&nodeID, &ownerNamespace)
			if errors.Is(err, pgx.ErrNoRows) || err == nil && ownerNamespace != namespace {
				return grpcstatus.Error(codes.NotFound, "Workspace not found")
			}
			if err != nil {
				return err
			}
			candidates = nil
			for _, candidate := range params.Candidates {
				if candidate != nil && candidate.Record != nil && candidate.Record.NodeID == nodeID {
					candidates = append(candidates, candidate)
				}
			}
			if len(candidates) == 0 {
				return grpcstatus.Error(codes.FailedPrecondition, "Workspace bound Node is unavailable for this Run")
			}
		}
		var binding *nodelifecyclev1.WorkspaceBinding
		var lockOwnership func(map[string]*nodekernel.Record) error
		if params.WorkspaceID != "" {
			boundNodeID := candidates[0].Record.NodeID
			lockOwnership = func(locked map[string]*nodekernel.Record) error {
				node := locked[boundNodeID]
				if node == nil || !node.Active() {
					return grpcstatus.Error(codes.FailedPrecondition, "Workspace bound Node is unavailable")
				}
				record, err := pgworkspace.LockForAdmission(ctx, tx, params.WorkspaceID, namespace, boundNodeID, node.Summary.GetWorkspaceStorage())
				if err != nil {
					return err
				}
				if err := pgaccess.AuthorizeTransaction(ctx, tx, accesskernel.ActionResourceWrite, namespace, now.Add(time.Since(started))); err != nil {
					return err
				}
				binding = record.Binding()
				return nil
			}
		}
		selected, err := s.admitCandidateResources(ctx, tx, resourceadmission.AdmitCandidateRequest{
			Namespace:              namespace,
			EnvironmentID:          params.Environment.GetID(),
			Candidates:             candidates,
			Config:                 params.Config,
			Now:                    now.Add(time.Since(started)),
			LockExecutionOwnership: lockOwnership,
		})
		if err != nil {
			return err
		}
		bindingJSON := "{}"
		var attachment *workspacev1.WorkspaceAttachment
		if binding != nil {
			bindingJSON, err = marshalProtoJSON(binding)
			if err != nil {
				return err
			}
			attachment = &workspacev1.WorkspaceAttachment{WorkspaceID: binding.GetSpec().GetWorkspaceID(), NodeID: binding.GetSpec().GetNodeID(), Generation: binding.GetGeneration(), Target: binding.GetTarget()}
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
			WorkspaceAttachment:     attachment,
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
		if normalizedConfig.GetRootfsSnapshot() != nil {
			run.RootfsSnapshot = &runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING}
		}
		snapshotResult := run.GetRootfsSnapshot()
		if snapshotResult == nil {
			snapshotResult = &runv1.RootfsSnapshotResult{}
		}
		rootfsSnapshotJSON, err := marshalProtoJSON(snapshotResult)
		if err != nil {
			return err
		}
		alloc = &runkernel.AllocationRecord{
			AllocationID:           run.GetAllocationID(),
			NodeID:                 selected.Record.NodeID,
			NodeTarget:             selected.Record.NodeTarget,
			CapabilityRequirements: selected.CapabilityRequirements,
			Workspace:              binding,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO runs (
				run_id, namespace, environment_id, status,
				config, environment_spec, resolved_environment_spec, labels, version, created_at, updated_at, message, rootfs_snapshot_result, workspace_binding
			) VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8::jsonb, $9, $10, $11, '', $12::jsonb, $13::jsonb)
		`, run.GetID(), run.GetNamespace(), run.GetEnvironmentID(), run.GetStatus().String(), cfgJSON, environmentSpecJSON, resolvedEnvironmentSpecJSON, labelsJSON, run.GetVersion(), now.UTC(), now.UTC(), rootfsSnapshotJSON, bindingJSON); err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		if err := insertRunSecretReferences(ctx, tx, run); err != nil {
			return err
		}
		resources := normalizedConfig.GetResources().GetRequests()
		if _, err := tx.Exec(ctx, `
			INSERT INTO allocations (
				allocation_id, run_id, node_id, lifecycle_state,
				cpu_request_milli, sandbox_memory_request_bytes, ephemeral_storage_request_bytes,
				created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, alloc.AllocationID, run.GetID(), alloc.NodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND.String(),
			resources.GetCpuMilli(), resources.GetMemoryBytes(), resources.GetEphemeralStorageBytes(), now.UTC(), now.UTC()); err != nil {
			return fmt.Errorf("insert allocation: %w", err)
		}
		if binding != nil {
			record := &workspacekernel.Record{Workspace: &workspacev1.Workspace{ID: binding.GetSpec().GetWorkspaceID(), Generation: binding.GetGeneration()}}
			if err := pgworkspace.AttachWriter(ctx, tx, record, alloc.AllocationID, now); err != nil {
				return err
			}
		}
		if err := pgallocation.InsertCapabilityRequirements(ctx, tx, alloc.AllocationID, selected.CapabilityRequirements, now); err != nil {
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
