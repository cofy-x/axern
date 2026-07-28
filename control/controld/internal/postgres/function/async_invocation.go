package pgfunction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errAsyncInvocationDeadlineElapsed = errors.New("asynchronous function invocation deadline elapsed during claim")

func (s *Store) claimAsyncInvocation(ctx context.Context, owner string, leaseTTL time.Duration) (*functionkernel.AsyncInvocationClaim, bool, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || leaseTTL <= 0 {
		return nil, false, fmt.Errorf("claim async function invocation: owner and positive lease ttl are required")
	}
	var claim *functionkernel.AsyncInvocationClaim
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var invocationID string
		err := tx.QueryRow(ctx, `
			SELECT i.invocation_id
			FROM function_invocations i
			JOIN functions f ON f.function_id=i.function_id AND f.active_revision_id=i.revision_id
			JOIN function_deployments d ON d.function_id=i.function_id AND d.active_revision_id=i.revision_id
			WHERE i.mode='FUNCTION_INVOCATION_MODE_ASYNC'
			  AND ((i.status='FUNCTION_INVOCATION_STATUS_QUEUED' AND i.next_run_at<=clock_timestamp())
			       OR (i.status='FUNCTION_INVOCATION_STATUS_RUNNING' AND i.lease_expires_at<=clock_timestamp()))
			  AND i.deadline_at>clock_timestamp()
			  AND f.status NOT IN ('FUNCTION_STATUS_DELETING','FUNCTION_STATUS_DELETED')
			  AND (SELECT count(*) FROM function_invocations active
			       WHERE active.function_id=i.function_id
			         AND active.status='FUNCTION_INVOCATION_STATUS_RUNNING'
			         AND (active.mode<>'FUNCTION_INVOCATION_MODE_ASYNC' OR active.lease_expires_at>clock_timestamp()))
			      < GREATEST(d.desired_replicas,1) * GREATEST(COALESCE(NULLIF((f.spec->'scaling'->>'concurrency')::int,0),1),1)
			ORDER BY i.next_run_at,i.created_at,i.invocation_id
			FOR UPDATE OF i,f,d SKIP LOCKED
			LIMIT 1
		`).Scan(&invocationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("select async function invocation: %w", err)
		}

		token := uuid.NewString()
		var generation int64
		var attempt int
		var claimedAt time.Time
		if err := tx.QueryRow(ctx, `
			UPDATE function_invocations
			SET status='FUNCTION_INVOCATION_STATUS_RUNNING',
			    started_at=COALESCE(started_at,clock_timestamp()),
			    attempt=attempt+1,
			    execution_generation=execution_generation+1,
			    claim_owner=$2,
			    lease_token_hash=$3,
			    lease_expires_at=LEAST(deadline_at,clock_timestamp()+make_interval(secs => $4::double precision)),
			    message=''
			WHERE invocation_id=$1
			RETURNING execution_generation,attempt,clock_timestamp()
		`, invocationID, owner, hashInvocationLeaseToken(token), leaseTTL.Seconds()).Scan(&generation, &attempt, &claimedAt); err != nil {
			return fmt.Errorf("lease async function invocation: %w", err)
		}
		invocation, err := scanInvocation(tx.QueryRow(ctx, invocationSelectSQL()+` WHERE invocation_id=$1`, invocationID))
		if err != nil {
			return fmt.Errorf("load claimed function invocation: %w", err)
		}
		fn, err := scanFunction(tx.QueryRow(ctx, functionSelectSQL()+` WHERE function_id=$1`, invocation.GetFunctionID()))
		if err != nil {
			return fmt.Errorf("load claimed function: %w", err)
		}
		revision, err := scanRevision(tx.QueryRow(ctx, revisionSelectSQL()+` WHERE revision_id=$1`, invocation.GetRevisionID()))
		if err != nil {
			return fmt.Errorf("load claimed function revision: %w", err)
		}
		deployment, err := scanDeployment(tx.QueryRow(ctx, deploymentSelectSQL()+` WHERE function_id=$1`, invocation.GetFunctionID()))
		if err != nil {
			return fmt.Errorf("load claimed function deployment: %w", err)
		}
		fn.Spec = functionkernel.CloneSpec(revision.GetSpec())
		if err := recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(
			invocation.GetFunctionID(),
			invocation.GetID(),
			invocation.GetRevisionID(),
			functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_INVOCATION_STARTED,
			"function invocation execution attempt started",
			map[string]string{
				"attempt":              fmt.Sprint(attempt),
				"execution_generation": fmt.Sprint(generation),
			},
			claimedAt,
		)); err != nil {
			return fmt.Errorf("record async function invocation start: %w", err)
		}
		var remainingSeconds float64
		if err := tx.QueryRow(ctx, `
			SELECT GREATEST(EXTRACT(EPOCH FROM deadline_at-clock_timestamp()),0)
			FROM function_invocations
			WHERE invocation_id=$1
		`, invocation.GetID()).Scan(&remainingSeconds); err != nil {
			return fmt.Errorf("load async function invocation deadline: %w", err)
		}
		if remainingSeconds <= 0 {
			return errAsyncInvocationDeadlineElapsed
		}
		claim = &functionkernel.AsyncInvocationClaim{
			Invocation:          invocation,
			Function:            fn,
			Revision:            revision,
			Deployment:          deployment,
			Owner:               owner,
			LeaseToken:          token,
			ExecutionGeneration: generation,
			Attempt:             attempt,
			DeadlineRemaining:   time.Duration(remainingSeconds * float64(time.Second)),
		}
		return nil
	})
	if errors.Is(err, errAsyncInvocationDeadlineElapsed) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return claim, claim != nil, nil
}

func (s *Store) renewAsyncInvocation(ctx context.Context, claim *functionkernel.AsyncInvocationClaim, leaseTTL time.Duration) (bool, error) {
	if claim == nil || claim.Invocation == nil || leaseTTL <= 0 {
		return false, nil
	}
	tag, err := s.db.Pool().Exec(ctx, `
		UPDATE function_invocations
		SET lease_expires_at=LEAST(deadline_at,clock_timestamp()+make_interval(secs => $5::double precision))
		WHERE invocation_id=$1
		  AND status='FUNCTION_INVOCATION_STATUS_RUNNING'
		  AND execution_generation=$2
		  AND claim_owner=$3
		  AND lease_token_hash=$4
		  AND lease_expires_at>clock_timestamp()
		  AND deadline_at>clock_timestamp()
	`, claim.Invocation.GetID(), claim.ExecutionGeneration, claim.Owner, hashInvocationLeaseToken(claim.LeaseToken), leaseTTL.Seconds())
	if err != nil {
		return false, fmt.Errorf("renew async function invocation: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) requeueAsyncInvocation(ctx context.Context, claim *functionkernel.AsyncInvocationClaim, delay time.Duration, message string) (bool, error) {
	if claim == nil || claim.Invocation == nil {
		return false, nil
	}
	if delay < 0 {
		delay = 0
	}
	requeued := false
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE function_invocations
			SET status='FUNCTION_INVOCATION_STATUS_QUEUED',
			    next_run_at=LEAST(deadline_at,clock_timestamp()+make_interval(secs => $5::double precision)),
			    claim_owner='',
			    lease_token_hash='',
			    lease_expires_at=NULL,
			    message=$6
			WHERE invocation_id=$1
			  AND status='FUNCTION_INVOCATION_STATUS_RUNNING'
			  AND execution_generation=$2
			  AND claim_owner=$3
			  AND lease_token_hash=$4
			  AND lease_expires_at>clock_timestamp()
			  AND deadline_at>clock_timestamp()
		`, claim.Invocation.GetID(), claim.ExecutionGeneration, claim.Owner, hashInvocationLeaseToken(claim.LeaseToken), delay.Seconds(), strings.TrimSpace(message))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return nil
		}
		if _, err := tx.Exec(ctx, `SELECT pg_notify($1,$2)`, functionInvocationChannel, claim.Invocation.GetID()); err != nil {
			return err
		}
		requeued = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("requeue async function invocation: %w", err)
	}
	return requeued, nil
}

func (s *Store) finishAsyncInvocation(ctx context.Context, claim *functionkernel.AsyncInvocationClaim, status functionv1.FunctionInvocationStatus, result *functionv1.FunctionResult, fnErr *functionv1.FunctionError, message string) (*functionv1.FunctionInvocation, bool, error) {
	if claim == nil || claim.Invocation == nil {
		return nil, false, nil
	}
	var finished *functionv1.FunctionInvocation
	var committed bool
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var generation int64
		var owner, tokenHash string
		var completedAt time.Time
		err := tx.QueryRow(ctx, `
			SELECT execution_generation,claim_owner,lease_token_hash,clock_timestamp()
			FROM function_invocations
			WHERE invocation_id=$1
			  AND status='FUNCTION_INVOCATION_STATUS_RUNNING'
			  AND lease_expires_at>clock_timestamp()
			  AND deadline_at>clock_timestamp()
			FOR UPDATE
		`, claim.Invocation.GetID()).Scan(&generation, &owner, &tokenHash, &completedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if generation != claim.ExecutionGeneration || owner != claim.Owner || tokenHash != hashInvocationLeaseToken(claim.LeaseToken) {
			return nil
		}
		current, err := scanInvocation(tx.QueryRow(ctx, invocationSelectSQL()+` WHERE invocation_id=$1`, claim.Invocation.GetID()))
		if err != nil {
			return err
		}
		finished = functionkernel.FinishInvocation(current, status, result, fnErr, message, completedAt)
		if err := updateInvocationTx(ctx, tx, finished); err != nil {
			return err
		}
		if err := decrementActiveInvocations(ctx, tx, finished.GetFunctionID(), completedAt); err != nil {
			return err
		}
		if err := recordInvocationFinishedEvent(ctx, tx, finished, status, message, completedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_notify($1,$2)`, functionInvocationChannel, finished.GetID()); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("finish async function invocation: %w", err)
	}
	return finished, committed, nil
}

func (s *Store) expireAsyncInvocations(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	expired := 0
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT invocation_id,clock_timestamp()
			FROM function_invocations
			WHERE mode='FUNCTION_INVOCATION_MODE_ASYNC'
			  AND status IN ('FUNCTION_INVOCATION_STATUS_QUEUED','FUNCTION_INVOCATION_STATUS_RUNNING')
			  AND deadline_at<=clock_timestamp()
			ORDER BY deadline_at,invocation_id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		type expiredInvocation struct {
			id          string
			completedAt time.Time
		}
		ids := make([]expiredInvocation, 0, limit)
		for rows.Next() {
			var item expiredInvocation
			if err := rows.Scan(&item.id, &item.completedAt); err != nil {
				return err
			}
			ids = append(ids, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		for _, item := range ids {
			current, err := scanInvocation(tx.QueryRow(ctx, invocationSelectSQL()+` WHERE invocation_id=$1`, item.id))
			if err != nil {
				return err
			}
			fnErr := &functionv1.FunctionError{Code: "timeout", Type: "Timeout", Message: "asynchronous function invocation deadline expired"}
			finished := functionkernel.FinishInvocation(current, functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_TIMED_OUT, nil, fnErr, fnErr.GetMessage(), item.completedAt)
			if err := updateInvocationTx(ctx, tx, finished); err != nil {
				return err
			}
			if err := decrementActiveInvocations(ctx, tx, current.GetFunctionID(), item.completedAt); err != nil {
				return err
			}
			if err := recordInvocationFinishedEvent(ctx, tx, finished, functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_TIMED_OUT, fnErr.GetMessage(), item.completedAt); err != nil {
				return err
			}
			expired++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("expire async function invocations: %w", err)
	}
	return expired, nil
}

func decrementActiveInvocations(ctx context.Context, tx pgx.Tx, functionID string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE function_deployments
		SET active_invocations=GREATEST(active_invocations-1,0),updated_at=$2
		WHERE function_id=$1
	`, functionID, now)
	return err
}

func recordInvocationFinishedEvent(ctx context.Context, tx pgx.Tx, invocation *functionv1.FunctionInvocation, status functionv1.FunctionInvocationStatus, message string, now time.Time) error {
	eventType := functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_INVOCATION_FAILED
	if status == functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED {
		eventType = functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_INVOCATION_SUCCEEDED
	}
	return recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(invocation.GetFunctionID(), invocation.GetID(), invocation.GetRevisionID(), eventType, message, nil, now))
}

func hashInvocationLeaseToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
