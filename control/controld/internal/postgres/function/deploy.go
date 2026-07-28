package pgfunction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) deployFunction(ctx context.Context, params functionkernel.DeployParams, now time.Time) (*functionkernel.DeployResult, error) {
	var result *functionkernel.DeployResult
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		deployed, err := s.deployFunctionTx(ctx, tx, params, now)
		if err != nil {
			return err
		}
		result = deployed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) deployFunctionTx(ctx context.Context, tx pgx.Tx, params functionkernel.DeployParams, now time.Time) (*functionkernel.DeployResult, error) {
	params.Namespace = functionkernel.NormalizeNamespace(params.Namespace)
	params.Name = functionkernel.NormalizeName(params.Name)
	sourceDigest := functionkernel.SourceDigest(params.Source)
	manifestDigest := functionkernel.ManifestDigest(params.Spec)

	if _, err := pgnamespace.Ensure(ctx, tx, params.Namespace); err != nil {
		return nil, err
	}
	if err := requireBundleTx(ctx, tx, params.Source.GetBundle()); err != nil {
		return nil, err
	}
	current, currentRevision, currentDeployment, ok, err := getFunctionTx(ctx, tx, "", params.Namespace, params.Name, true)
	if err != nil {
		return nil, err
	}
	if deployIsCurrent(current, currentRevision, sourceDigest, manifestDigest) {
		return &functionkernel.DeployResult{Function: current, Revision: currentRevision, Deployment: currentDeployment}, nil
	}

	if !ok {
		revision := functionkernel.NewRevision("", 1, params, sourceDigest, manifestDigest, now)
		fn := functionkernel.NewFunction(params, revision.GetID(), now)
		revision.FunctionID = fn.GetID()
		deployment := functionkernel.NewDeployment(fn.GetID(), revision.GetID(), functionScaling(params.Spec), now)
		if err := insertFunctionTx(ctx, tx, fn); err != nil {
			return nil, err
		}
		if err := insertRevisionTx(ctx, tx, revision); err != nil {
			return nil, err
		}
		if err := upsertDeploymentTx(ctx, tx, deployment); err != nil {
			return nil, err
		}
		if err := recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(fn.GetID(), "", revision.GetID(), functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_REVISION_CREATED, "function revision created", nil, now)); err != nil {
			return nil, err
		}
		if err := recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(fn.GetID(), "", revision.GetID(), functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_REVISION_ACTIVATED, "function revision activated", nil, now)); err != nil {
			return nil, err
		}
		return &functionkernel.DeployResult{Function: fn, Revision: revision, Deployment: deployment}, nil
	}
	if currentDeployment != nil && currentDeployment.GetActiveInvocations() > 0 {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "function revision cannot change while invocations are active")
	}

	revisionNumber, err := nextRevisionNumberTx(ctx, tx, current.GetID())
	if err != nil {
		return nil, err
	}
	revision := functionkernel.NewRevision(current.GetID(), revisionNumber, params, sourceDigest, manifestDigest, now)
	nextFunction := functionkernel.CloneFunction(current)
	nextFunction.ActiveRevisionID = revision.GetID()
	nextFunction.Spec = functionkernel.CloneSpec(params.Spec)
	nextFunction.Status = functionv1.FunctionStatus_FUNCTION_STATUS_DEPLOYING
	nextFunction.DeploymentStatus = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_PENDING
	nextFunction.Labels = functionkernel.CloneLabels(params.Labels)
	nextFunction.Version++
	nextFunction.UpdatedAt = revision.GetCreatedAt()
	nextFunction.Message = "function deploy persisted; worker rollout pending"
	deployment := functionkernel.NewDeployment(nextFunction.GetID(), revision.GetID(), functionScaling(params.Spec), now)
	deployment.WorkerServiceID = reusableWorkerServiceID(current, currentDeployment)
	if err := updateFunctionTx(ctx, tx, nextFunction); err != nil {
		return nil, err
	}
	if err := insertRevisionTx(ctx, tx, revision); err != nil {
		return nil, err
	}
	if err := upsertDeploymentTx(ctx, tx, deployment); err != nil {
		return nil, err
	}
	if err := recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(nextFunction.GetID(), "", revision.GetID(), functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_REVISION_CREATED, "function revision created", nil, now)); err != nil {
		return nil, err
	}
	if err := recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(nextFunction.GetID(), "", revision.GetID(), functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_REVISION_ACTIVATED, "function revision activated", nil, now)); err != nil {
		return nil, err
	}
	return &functionkernel.DeployResult{Function: nextFunction, Revision: revision, Deployment: deployment}, nil
}

func deployIsCurrent(current *functionv1.Function, revision *functionv1.FunctionRevision, sourceDigest, manifestDigest string) bool {
	if current == nil || revision == nil {
		return false
	}
	if current.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_DELETED ||
		current.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_DELETING {
		return false
	}
	return revision.GetSourceDigest() == sourceDigest && revision.GetManifestDigest() == manifestDigest
}

func reusableWorkerServiceID(current *functionv1.Function, deployment *functionv1.FunctionDeployment) string {
	if current == nil || deployment == nil ||
		current.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_DELETED ||
		current.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_DELETING {
		return ""
	}
	return deployment.GetWorkerServiceID()
}

func functionScaling(spec *functionv1.FunctionSpec) *functionv1.FunctionScalingSpec {
	if spec == nil {
		return nil
	}
	return spec.GetScaling()
}

func insertFunctionTx(ctx context.Context, tx pgx.Tx, fn *functionv1.Function) error {
	specJSON, err := marshalProtoJSON(fn.GetSpec())
	if err != nil {
		return err
	}
	labelsJSON, err := marshalJSONMap(fn.GetLabels())
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO functions (
			function_id, namespace, name, active_revision_id, spec, status, deployment_status, labels, version, created_at, updated_at, message, diagnostic_code
		) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8::jsonb, $9, $10, $11, $12, $13)
	`, fn.GetID(), fn.GetNamespace(), fn.GetName(), fn.GetActiveRevisionID(), specJSON, fn.GetStatus().String(), fn.GetDeploymentStatus().String(), labelsJSON, fn.GetVersion(), fn.GetCreatedAt().AsTime().UTC(), fn.GetUpdatedAt().AsTime().UTC(), fn.GetMessage(), fn.GetDiagnosticCode().String()); err != nil {
		return fmt.Errorf("insert function: %w", err)
	}
	return nil
}

func updateFunctionTx(ctx context.Context, tx pgx.Tx, fn *functionv1.Function) error {
	specJSON, err := marshalProtoJSON(fn.GetSpec())
	if err != nil {
		return err
	}
	labelsJSON, err := marshalJSONMap(fn.GetLabels())
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE functions
		SET active_revision_id = $2, spec = $3::jsonb, status = $4, deployment_status = $5, labels = $6::jsonb, version = $7, updated_at = $8, message = $9, diagnostic_code = $10
		WHERE function_id = $1
	`, fn.GetID(), fn.GetActiveRevisionID(), specJSON, fn.GetStatus().String(), fn.GetDeploymentStatus().String(), labelsJSON, fn.GetVersion(), fn.GetUpdatedAt().AsTime().UTC(), fn.GetMessage(), fn.GetDiagnosticCode().String()); err != nil {
		return fmt.Errorf("update function: %w", err)
	}
	return nil
}

func insertRevisionTx(ctx context.Context, tx pgx.Tx, revision *functionv1.FunctionRevision) error {
	specJSON, err := marshalProtoJSON(revision.GetSpec())
	if err != nil {
		return err
	}
	sourceJSON, err := marshalProtoJSON(revision.GetSource())
	if err != nil {
		return err
	}
	labelsJSON, err := marshalJSONMap(revision.GetLabels())
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO function_revisions (
			revision_id, function_id, namespace, name, revision_number, spec, source, source_digest, manifest_digest, labels, created_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10::jsonb, $11, $12)
	`, revision.GetID(), revision.GetFunctionID(), revision.GetNamespace(), revision.GetName(), revision.GetRevisionNumber(), specJSON, sourceJSON, revision.GetSourceDigest(), revision.GetManifestDigest(), labelsJSON, revision.GetCreatedAt().AsTime().UTC(), revision.GetCreatedBy()); err != nil {
		return fmt.Errorf("insert function revision: %w", err)
	}
	return nil
}

func upsertDeploymentTx(ctx context.Context, tx pgx.Tx, deployment *functionv1.FunctionDeployment) error {
	scalingJSON, err := marshalProtoJSON(deployment.GetScaling())
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO function_deployments (
			function_id, active_revision_id, status, scaling, desired_replicas, ready_replicas, active_invocations, updated_at, message, diagnostic_code, worker_service_id
		) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (function_id) DO UPDATE
		SET active_revision_id = EXCLUDED.active_revision_id,
			status = EXCLUDED.status,
			scaling = EXCLUDED.scaling,
			desired_replicas = EXCLUDED.desired_replicas,
			ready_replicas = EXCLUDED.ready_replicas,
			active_invocations = EXCLUDED.active_invocations,
			updated_at = EXCLUDED.updated_at,
			message = EXCLUDED.message,
			diagnostic_code = EXCLUDED.diagnostic_code,
			worker_service_id = EXCLUDED.worker_service_id
	`, deployment.GetFunctionID(), deployment.GetActiveRevisionID(), deployment.GetStatus().String(), scalingJSON, deployment.GetDesiredReplicas(), deployment.GetReadyReplicas(), deployment.GetActiveInvocations(), deployment.GetUpdatedAt().AsTime().UTC(), deployment.GetMessage(), deployment.GetDiagnosticCode().String(), deployment.GetWorkerServiceID()); err != nil {
		return fmt.Errorf("upsert function deployment: %w", err)
	}
	return nil
}

func nextRevisionNumberTx(ctx context.Context, tx pgx.Tx, functionID string) (int64, error) {
	var current int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision_number), 0) FROM function_revisions WHERE function_id = $1`, strings.TrimSpace(functionID)).Scan(&current); err != nil {
		return 0, fmt.Errorf("select next function revision: %w", err)
	}
	return current + 1, nil
}

func (s *Store) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return err
	}
	return nil
}
