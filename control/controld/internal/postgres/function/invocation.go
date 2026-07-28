package pgfunction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) startInvocation(ctx context.Context, params functionkernel.InvokeParams, now time.Time) (*functionkernel.InvocationStartResult, error) {
	result := &functionkernel.InvocationStartResult{}
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, activeRevision, currentDeployment, found, err := getFunctionTx(ctx, tx, params.FunctionID, params.Namespace, params.Name, true)
		if err != nil || !found {
			result.Found = found
			return err
		}
		if current.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_DELETED || current.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_DELETING {
			return grpcstatus.Error(codes.FailedPrecondition, "function is deleted")
		}
		if activeRevision == nil {
			return grpcstatus.Error(codes.FailedPrecondition, "function active revision is missing")
		}
		if revisionID := strings.TrimSpace(params.RevisionID); revisionID != "" && revisionID != activeRevision.GetID() {
			return grpcstatus.Error(codes.FailedPrecondition, "requested function revision is not active")
		}

		requestID := strings.TrimSpace(params.RequestID)
		if requestID != "" {
			existing, replay, idempErr := claimIdempotencyTx(ctx, tx, current, activeRevision, requestID, now)
			if idempErr != nil {
				return idempErr
			}
			if replay && existing != nil {
				result.Invocation = existing
				result.Function = current
				result.Revision = activeRevision
				result.Deployment = currentDeployment
				result.Found = true
				result.Replay = true
				return nil
			}
		}

		effectiveTimeout := invocationTimeout(current, params.Timeout)
		params.Timeout = durationpb.New(effectiveTimeout)
		invocation := functionkernel.NewInvocation(current, activeRevision, params, now)
		if err := insertInvocationTx(ctx, tx, invocation, effectiveTimeout); err != nil {
			return err
		}

		if requestID != "" {
			if err := setIdempotencyInvocationTx(ctx, tx, current.GetNamespace(), current.GetID(), activeRevision.GetID(), requestID, invocation.GetID()); err != nil {
				return err
			}
		}

		if currentDeployment != nil {
			currentDeployment.ActiveInvocations++
			currentDeployment.UpdatedAt = invocation.GetCreatedAt()
			if err := upsertDeploymentTx(ctx, tx, currentDeployment); err != nil {
				return err
			}
		}
		if params.Mode != functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC {
			if err := recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(current.GetID(), invocation.GetID(), activeRevision.GetID(), functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_INVOCATION_STARTED, "function invocation started", nil, invocation.GetCreatedAt().AsTime())); err != nil {
				return err
			}
		}
		if params.Mode == functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC {
			if _, err := tx.Exec(ctx, `SELECT pg_notify($1,$2)`, functionInvocationChannel, invocation.GetID()); err != nil {
				return fmt.Errorf("notify async function invocation: %w", err)
			}
		}
		result.Invocation = invocation
		result.Function = current
		result.Revision = activeRevision
		result.Deployment = currentDeployment
		result.Found = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("start function invocation: %w", err)
	}
	return result, nil
}

func claimIdempotencyTx(ctx context.Context, tx pgx.Tx, fn *functionv1.Function, revision *functionv1.FunctionRevision, requestID string, now time.Time) (*functionv1.FunctionInvocation, bool, error) {
	var existingInvocationID string
	err := tx.QueryRow(ctx, `
		INSERT INTO function_idempotency_records (namespace, function_id, revision_id, request_id, invocation_id, created_at)
		VALUES ($1, $2, $3, $4, '', $5)
		ON CONFLICT (namespace, function_id, revision_id, request_id) DO UPDATE SET namespace = function_idempotency_records.namespace
		RETURNING invocation_id
	`, fn.GetNamespace(), fn.GetID(), revision.GetID(), requestID, now.UTC()).Scan(&existingInvocationID)
	if err != nil {
		return nil, false, fmt.Errorf("claim idempotency record: %w", err)
	}
	if existingInvocationID == "" {
		return nil, false, nil
	}
	existing, scanErr := scanInvocation(tx.QueryRow(ctx, invocationSelectSQL()+` WHERE invocation_id = $1`, existingInvocationID))
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if scanErr != nil {
		return nil, false, fmt.Errorf("get idempotent invocation: %w", scanErr)
	}
	return existing, true, nil
}

func setIdempotencyInvocationTx(ctx context.Context, tx pgx.Tx, namespace, functionID, revisionID, requestID, invocationID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE function_idempotency_records SET invocation_id = $5
		WHERE namespace = $1 AND function_id = $2 AND revision_id = $3 AND request_id = $4
	`, namespace, functionID, revisionID, requestID, invocationID)
	if err != nil {
		return fmt.Errorf("set idempotency invocation_id: %w", err)
	}
	return nil
}

func (s *Store) finishInvocation(ctx context.Context, invocationID string, status functionv1.FunctionInvocationStatus, result *functionv1.FunctionResult, fnErr *functionv1.FunctionError, message string, now time.Time) (*functionv1.FunctionInvocation, bool, error) {
	var (
		next *functionv1.FunctionInvocation
		ok   bool
	)
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanInvocation(tx.QueryRow(ctx, invocationSelectSQL()+` WHERE invocation_id = $1 FOR UPDATE`, strings.TrimSpace(invocationID)))
		if errors.Is(err, pgx.ErrNoRows) {
			ok = false
			return nil
		}
		if err != nil {
			return fmt.Errorf("get function invocation: %w", err)
		}
		next = functionkernel.FinishInvocation(current, status, result, fnErr, message, now)
		if err := updateInvocationTx(ctx, tx, next); err != nil {
			return err
		}
		deployment, err := scanDeployment(tx.QueryRow(ctx, deploymentSelectSQL()+` WHERE function_id = $1 FOR UPDATE`, next.GetFunctionID()))
		if errors.Is(err, pgx.ErrNoRows) {
			deployment = nil
		} else if err != nil {
			return fmt.Errorf("get function deployment: %w", err)
		}
		if deployment != nil {
			if deployment.ActiveInvocations > 0 {
				deployment.ActiveInvocations--
			}
			deployment.UpdatedAt = next.GetCompletedAt()
			if err := upsertDeploymentTx(ctx, tx, deployment); err != nil {
				return err
			}
		}
		eventType := functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_INVOCATION_FAILED
		if status == functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED {
			eventType = functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_INVOCATION_SUCCEEDED
		}
		if err := recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(next.GetFunctionID(), next.GetID(), next.GetRevisionID(), eventType, message, nil, now)); err != nil {
			return err
		}
		ok = true
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("finish function invocation: %w", err)
	}
	return next, ok, nil
}

func insertInvocationTx(ctx context.Context, tx pgx.Tx, invocation *functionv1.FunctionInvocation, timeout time.Duration) error {
	payloadJSON, err := marshalProtoJSON(invocation.GetPayload())
	if err != nil {
		return err
	}
	resultJSON, err := marshalProtoJSON(invocation.GetResult())
	if err != nil {
		return err
	}
	errorJSON, err := marshalProtoJSON(invocation.GetError())
	if err != nil {
		return err
	}
	timeoutJSON, err := marshalProtoJSON(invocation.GetTimeout())
	if err != nil {
		return err
	}
	durationJSON, err := marshalProtoJSON(invocation.GetDuration())
	if err != nil {
		return err
	}
	labelsJSON, err := marshalJSONMap(invocation.GetLabels())
	if err != nil {
		return err
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO function_invocations (
			invocation_id, function_id, function_name, namespace, revision_id, status, mode, payload, result, error, timeout, duration, request_id, labels, created_at, started_at, completed_at, next_run_at, deadline_at, message, diagnostic_code
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb, $12::jsonb, $13, $14::jsonb,
		          clock_timestamp(), CASE WHEN $15 THEN clock_timestamp() ELSE NULL END, NULL,
		          clock_timestamp(), clock_timestamp()+make_interval(secs => $16::double precision), $17, $18)
		RETURNING created_at
	`, invocation.GetID(), invocation.GetFunctionID(), invocation.GetFunctionName(), invocation.GetNamespace(), invocation.GetRevisionID(), invocation.GetStatus().String(), invocation.GetMode().String(), payloadJSON, resultJSON, errorJSON, timeoutJSON, durationJSON, invocation.GetRequestID(), labelsJSON, invocation.GetMode() != functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC, timeout.Seconds(), invocation.GetMessage(), invocation.GetDiagnosticCode().String()).Scan(&createdAt); err != nil {
		return fmt.Errorf("insert function invocation: %w", err)
	}
	invocation.CreatedAt = timestamppb.New(createdAt.UTC())
	if invocation.GetMode() != functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC {
		invocation.StartedAt = invocation.GetCreatedAt()
	}
	return nil
}

func invocationTimeout(fn *functionv1.Function, requested *durationpb.Duration) time.Duration {
	timeout := 30 * time.Second
	if configured := fn.GetSpec().GetTimeout(); configured != nil && configured.CheckValid() == nil && configured.AsDuration() > 0 {
		timeout = configured.AsDuration()
	}
	if requested != nil && requested.CheckValid() == nil && requested.AsDuration() > 0 {
		timeout = requested.AsDuration()
	}
	return timeout
}

func updateInvocationTx(ctx context.Context, tx pgx.Tx, invocation *functionv1.FunctionInvocation) error {
	resultJSON, err := marshalProtoJSON(invocation.GetResult())
	if err != nil {
		return err
	}
	errorJSON, err := marshalProtoJSON(invocation.GetError())
	if err != nil {
		return err
	}
	durationJSON, err := marshalProtoJSON(invocation.GetDuration())
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE function_invocations
		SET status = $2, result = $3::jsonb, error = $4::jsonb, duration = $5::jsonb, completed_at = $6, message = $7, diagnostic_code = $8,
		    claim_owner = '', lease_token_hash = '', lease_expires_at = NULL
		WHERE invocation_id = $1
	`, invocation.GetID(), invocation.GetStatus().String(), resultJSON, errorJSON, durationJSON, invocation.GetCompletedAt().AsTime().UTC(), invocation.GetMessage(), invocation.GetDiagnosticCode().String()); err != nil {
		return fmt.Errorf("update function invocation: %w", err)
	}
	return nil
}
