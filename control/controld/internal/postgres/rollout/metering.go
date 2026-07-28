package pgrollout

import (
	"context"
	"fmt"
	"math"
	"time"

	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) ResolveAgentProfile(ctx context.Context, req *workerrolloutv1.ResolveAgentProfileRequest, leaseTokenHash string, now time.Time) (*workerrolloutv1.ResolvedAgentProfile, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockActiveWork(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return nil, err
	}
	var profileJSON []byte
	var credentialSecretID string
	if work.kind == "WORK_KIND_PROFILE_DOCTOR" {
		if err := tx.QueryRow(ctx, `SELECT frozen_profile,frozen_credential_secret_id FROM agent_profile_doctor_jobs WHERE job_id=$1`, work.doctorJobID).Scan(&profileJSON, &credentialSecretID); err != nil {
			return nil, err
		}
	} else if err := tx.QueryRow(ctx, `SELECT COALESCE(frozen_profile,'{}'::jsonb),COALESCE(frozen_credential_secret_id,'') FROM rollouts WHERE rollout_id=$1`, work.rolloutID).Scan(&profileJSON, &credentialSecretID); err != nil {
		return nil, err
	}
	if credentialSecretID == "" {
		return nil, status.Error(codes.FailedPrecondition, "rollout does not use an agent profile")
	}
	profile := &agentprofilev1.AgentProfile{}
	if err := protojson.Unmarshal(profileJSON, profile); err != nil {
		return nil, fmt.Errorf("unmarshal frozen agent profile: %w", err)
	}
	secret, ok, err := s.secrets.ResolveProfileCredential(ctx, credentialSecretID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "agent credential secret no longer exists")
	}
	token := secret.Data["token"]
	if token == "" {
		return nil, status.Error(codes.FailedPrecondition, "agent credential secret has no token")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &workerrolloutv1.ResolvedAgentProfile{Profile: profile, Token: token}, nil
}

func (s *Store) ReserveUsage(ctx context.Context, req *workerrolloutv1.ReserveUsageRequest, leaseTokenHash string, now time.Time) (int64, int64, error) {
	if req == nil || req.GetReservationID() == "" || req.GetMaxTokens() < 0 || req.GetMaxCostMicrousd() < 0 {
		return 0, 0, status.Error(codes.InvalidArgument, "reservation_id and non-negative limits are required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockActiveWork(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return 0, 0, err
	}
	budget, err := lockBudget(ctx, tx, work.rolloutID)
	if err != nil {
		return 0, 0, err
	}
	var existingEpisode, existingStatus string
	var existingGeneration, existingTokens, existingCost int64
	err = tx.QueryRow(ctx, `SELECT COALESCE(episode_id,''),execution_generation,reserved_tokens,reserved_cost_microusd,status FROM rollout_usage_reservations WHERE reservation_id=$1`, req.GetReservationID()).Scan(&existingEpisode, &existingGeneration, &existingTokens, &existingCost, &existingStatus)
	if err == nil {
		if existingEpisode != work.episodeID || existingGeneration != work.generation || existingTokens != req.GetMaxTokens() || existingCost != req.GetMaxCostMicrousd() {
			return 0, 0, status.Error(codes.AlreadyExists, "reservation_id already exists with different parameters")
		}
		if existingStatus == "RELEASED" {
			return 0, 0, status.Error(codes.FailedPrecondition, "usage reservation has already been released")
		}
		tokens, cost, err := remainingBudget(ctx, tx, work.rolloutID, budget)
		if err != nil {
			return 0, 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, 0, err
		}
		return tokens, cost, nil
	} else if err != pgx.ErrNoRows {
		return 0, 0, err
	}
	remainingTokens, remainingCost, err := remainingBudget(ctx, tx, work.rolloutID, budget)
	if err != nil {
		return 0, 0, err
	}
	if (budget.GetMaxTokens() > 0 && req.GetMaxTokens() > remainingTokens) || (budget.GetMaxCostMicrousd() > 0 && req.GetMaxCostMicrousd() > remainingCost) {
		return 0, 0, status.Error(codes.ResourceExhausted, "rollout usage budget is exhausted")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO rollout_usage_reservations(reservation_id,rollout_id,episode_id,execution_generation,reserved_tokens,reserved_cost_microusd,status,created_at) VALUES($1,$2,NULLIF($3,''),$4,$5,$6,'RESERVED',$7)`, req.GetReservationID(), work.rolloutID, work.episodeID, work.generation, req.GetMaxTokens(), req.GetMaxCostMicrousd(), now.UTC()); err != nil {
		return 0, 0, err
	}
	remainingTokens, remainingCost, err = remainingBudget(ctx, tx, work.rolloutID, budget)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return remainingTokens, remainingCost, nil
}

func (s *Store) CommitUsage(ctx context.Context, req *workerrolloutv1.CommitUsageRequest, leaseTokenHash string, now time.Time) (int64, int64, error) {
	if req == nil || req.GetReservationID() == "" || req.GetInputTokens() < 0 || req.GetCachedInputTokens() < 0 || req.GetOutputTokens() < 0 || req.GetCostMicrousd() < 0 {
		return 0, 0, status.Error(codes.InvalidArgument, "reservation_id and non-negative actual usage are required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockActiveWork(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return 0, 0, err
	}
	budget, err := lockBudget(ctx, tx, work.rolloutID)
	if err != nil {
		return 0, 0, err
	}
	var episodeID, statusText string
	var generation, reservedTokens, reservedCost, input, cached, output, cost int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(episode_id,''),execution_generation,reserved_tokens,reserved_cost_microusd,status,actual_input_tokens,actual_cached_input_tokens,actual_output_tokens,actual_cost_microusd FROM rollout_usage_reservations WHERE reservation_id=$1 FOR UPDATE`, req.GetReservationID()).Scan(&episodeID, &generation, &reservedTokens, &reservedCost, &statusText, &input, &cached, &output, &cost); err != nil {
		if err == pgx.ErrNoRows {
			return 0, 0, status.Error(codes.NotFound, "usage reservation not found")
		}
		return 0, 0, err
	}
	if episodeID != work.episodeID || generation != work.generation {
		return 0, 0, status.Error(codes.FailedPrecondition, "usage reservation does not belong to work lease")
	}
	actualTokens := req.GetInputTokens() + req.GetOutputTokens()
	overReservation := (budget.GetMaxTokens() > 0 && actualTokens > reservedTokens) || (budget.GetMaxCostMicrousd() > 0 && req.GetCostMicrousd() > reservedCost)
	switch statusText {
	case "COMMITTED":
		if input != req.GetInputTokens() || cached != req.GetCachedInputTokens() || output != req.GetOutputTokens() || cost != req.GetCostMicrousd() {
			return 0, 0, status.Error(codes.AlreadyExists, "usage reservation committed with different values")
		}
	case "RESERVED":
		if _, err := tx.Exec(ctx, `UPDATE rollout_usage_reservations SET status='COMMITTED',actual_input_tokens=$2,actual_cached_input_tokens=$3,actual_output_tokens=$4,actual_cost_microusd=$5,completed_at=$6 WHERE reservation_id=$1 AND status='RESERVED'`, req.GetReservationID(), req.GetInputTokens(), req.GetCachedInputTokens(), req.GetOutputTokens(), req.GetCostMicrousd(), now.UTC()); err != nil {
			return 0, 0, err
		}
	default:
		return 0, 0, status.Error(codes.FailedPrecondition, "usage reservation cannot be committed")
	}
	remainingTokens, remainingCost, err := remainingBudget(ctx, tx, work.rolloutID, budget)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	if overReservation {
		return remainingTokens, remainingCost, status.Error(codes.ResourceExhausted, "actual usage exceeds reserved rollout budget allowance")
	}
	return remainingTokens, remainingCost, nil
}

func (s *Store) ReleaseUsage(ctx context.Context, req *workerrolloutv1.ReleaseUsageRequest, leaseTokenHash string, now time.Time) error {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockActiveWork(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE rollout_usage_reservations SET status='RELEASED',completed_at=$2 WHERE reservation_id=$1 AND rollout_id=$3 AND episode_id IS NOT DISTINCT FROM NULLIF($4,'') AND execution_generation=$5 AND status='RESERVED'`, req.GetReservationID(), now.UTC(), work.rolloutID, work.episodeID, work.generation)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var statusText string
		if err := tx.QueryRow(ctx, `SELECT status FROM rollout_usage_reservations WHERE reservation_id=$1`, req.GetReservationID()).Scan(&statusText); err != nil {
			if err == pgx.ErrNoRows {
				return status.Error(codes.NotFound, "usage reservation not found")
			}
			return err
		}
		if statusText != "RELEASED" {
			return status.Error(codes.FailedPrecondition, "usage reservation cannot be released")
		}
	}
	return tx.Commit(ctx)
}

func lockBudget(ctx context.Context, tx pgx.Tx, rolloutID string) (*rolloutv1.RolloutBudget, error) {
	var specJSON []byte
	if err := tx.QueryRow(ctx, `SELECT spec FROM rollouts WHERE rollout_id=$1 FOR UPDATE`, rolloutID).Scan(&specJSON); err != nil {
		return nil, err
	}
	spec := &rolloutv1.RolloutSpec{}
	if err := protojson.Unmarshal(specJSON, spec); err != nil {
		return nil, fmt.Errorf("decode rollout budget: %w", err)
	}
	if spec.GetBudget() == nil {
		return &rolloutv1.RolloutBudget{}, nil
	}
	return spec.GetBudget(), nil
}
func remainingBudget(ctx context.Context, tx pgx.Tx, rolloutID string, budget *rolloutv1.RolloutBudget) (int64, int64, error) {
	var tokens, cost int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN status='COMMITTED' THEN actual_input_tokens+actual_output_tokens WHEN status='RESERVED' THEN reserved_tokens ELSE 0 END),0),COALESCE(sum(CASE WHEN status='COMMITTED' THEN actual_cost_microusd WHEN status='RESERVED' THEN reserved_cost_microusd ELSE 0 END),0) FROM rollout_usage_reservations WHERE rollout_id=$1`, rolloutID).Scan(&tokens, &cost); err != nil {
		return 0, 0, err
	}
	remainingTokens := int64(math.MaxInt64)
	remainingCost := int64(math.MaxInt64)
	if budget.GetMaxTokens() > 0 {
		remainingTokens = max(0, budget.GetMaxTokens()-tokens)
	}
	if budget.GetMaxCostMicrousd() > 0 {
		remainingCost = max(0, budget.GetMaxCostMicrousd()-cost)
	}
	return remainingTokens, remainingCost, nil
}

func validateCommittedUsageTx(ctx context.Context, tx pgx.Tx, work *workRecord, reservationID string, inputTokens, cachedInputTokens, outputTokens, costMicrousd int64, required bool) error {
	if !required {
		if reservationID != "" {
			return status.Error(codes.InvalidArgument, "usage reservation is not valid for unmetered work")
		}
		return nil
	}
	if reservationID == "" {
		return status.Error(codes.FailedPrecondition, "managed work requires a committed usage reservation")
	}
	var statusText string
	var actualInput, actualCached, actualOutput, actualCost int64
	err := tx.QueryRow(ctx, `SELECT status,actual_input_tokens,actual_cached_input_tokens,actual_output_tokens,actual_cost_microusd
		FROM rollout_usage_reservations
		WHERE reservation_id=$1 AND rollout_id=$2 AND episode_id IS NOT DISTINCT FROM NULLIF($3,'') AND execution_generation=$4
		FOR UPDATE`, reservationID, work.rolloutID, work.episodeID, work.generation).Scan(&statusText, &actualInput, &actualCached, &actualOutput, &actualCost)
	if err == pgx.ErrNoRows {
		return status.Error(codes.FailedPrecondition, "usage reservation does not belong to work generation")
	}
	if err != nil {
		return err
	}
	if statusText != "COMMITTED" {
		return status.Error(codes.FailedPrecondition, "usage reservation is not committed")
	}
	if actualInput != inputTokens || actualCached != cachedInputTokens || actualOutput != outputTokens || actualCost != costMicrousd {
		return status.Error(codes.FailedPrecondition, "reported usage does not match committed usage reservation")
	}
	return nil
}
