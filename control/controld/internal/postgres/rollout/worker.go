package pgrollout

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workRecord struct {
	id, kind, rolloutID, episodeID, doctorJobID string
	generation                                  int64
	status, leaseTokenHash                      string
	leaseExpiresAt                              *time.Time
	cancelRequested                             bool
	resultDigest                                string
	attempts                                    int
	requiredProfileID                           string
	requiredProfileVersion                      int64
	requiredProfileConcurrency                  int
	nextRunAt                                   time.Time
	payload                                     []byte
}

const claimableWorkFromSQL = `
	FROM rollout_work_items w
	LEFT JOIN rollouts r ON r.rollout_id=w.rollout_id
	WHERE ((w.status='PENDING' AND w.next_run_at<=$1) OR (w.status='LEASED' AND w.lease_expires_at<=$1))
	  AND w.cancel_requested=FALSE
	  AND (($2 AND w.kind IN ('WORK_KIND_PLAN','WORK_KIND_PROFILE_DOCTOR') AND (w.required_wire_api='' OR w.required_wire_api=ANY($6)))
	       OR (w.kind='WORK_KIND_EPISODE' AND w.required_agent=ANY($3) AND (w.required_wire_api='' OR w.required_wire_api=ANY($6))))
	  AND (SELECT count(*) FROM rollout_work_items owned WHERE owned.status='LEASED' AND owned.claim_owner=$4 AND owned.lease_expires_at>$1) < $5
	  AND (w.kind IN ('WORK_KIND_PLAN','WORK_KIND_PROFILE_DOCTOR') OR (SELECT count(*) FROM rollout_work_items rollout_work WHERE rollout_work.status='LEASED' AND rollout_work.lease_expires_at>$1 AND rollout_work.rollout_id=w.rollout_id) < COALESCE(NULLIF((r.spec->'execution'->>'concurrency')::int,0),1))
	  AND (w.required_profile_id='' OR (SELECT count(*) FROM rollout_work_items profile_work WHERE profile_work.status='LEASED' AND profile_work.required_profile_id<>'' AND profile_work.lease_expires_at>$1 AND profile_work.required_profile_id=w.required_profile_id AND profile_work.required_profile_version=w.required_profile_version) < w.required_profile_concurrency)
`

func (s *Store) RegisterWorker(ctx context.Context, req *workerrolloutv1.RegisterWorkerRequest, tokenHash string, now time.Time, ttl time.Duration) (*workerrolloutv1.RegisterWorkerResponse, error) {
	if req == nil || strings.TrimSpace(req.GetWorkerID()) == "" || req.GetCapabilities() == nil || req.GetCapabilities().GetMaxConcurrency() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "worker_id and positive capabilities.max_concurrency are required")
	}
	if !req.GetCapabilities().GetPlanner() && len(req.GetCapabilities().GetAgents()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "worker must support planning or at least one agent")
	}
	if tokenHash == "" {
		return nil, status.Error(codes.Unauthenticated, "worker bootstrap credential was not validated")
	}
	sessionToken, err := randomToken()
	if err != nil {
		return nil, err
	}
	sessionID := "wss-" + uuid.NewString()
	expiresAt := now.UTC().Add(ttl)
	capabilities, err := protojson.Marshal(req.GetCapabilities())
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Pool().Exec(ctx, `INSERT INTO rollout_worker_sessions(session_id,worker_id,token_hash,capabilities,expires_at,last_heartbeat_at,created_at) VALUES($1,$2,$3,$4::jsonb,$5,$6,$6)`, sessionID, strings.TrimSpace(req.GetWorkerID()), rolloutkernel.HashToken(sessionToken), capabilities, expiresAt, now.UTC()); err != nil {
		return nil, fmt.Errorf("register rollout worker: %w", err)
	}
	return &workerrolloutv1.RegisterWorkerResponse{SessionID: sessionID, SessionToken: sessionToken, ExpiresAt: timestamppb.New(expiresAt)}, nil
}

func (s *Store) ClaimWork(ctx context.Context, sessionID, sessionTokenHash string, now time.Time, ttl time.Duration) (item *workerrolloutv1.WorkItem, leaseToken string, err error) {
	started := time.Now()
	claimResult := workClaimResultError
	defer func() { recordRolloutWorkClaim(ctx, claimResult, started, err) }()
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var workerID string
	var capabilitiesJSON []byte
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, `SELECT worker_id,capabilities,expires_at FROM rollout_worker_sessions WHERE session_id=$1 AND token_hash=$2 FOR UPDATE`, sessionID, sessionTokenHash).Scan(&workerID, &capabilitiesJSON, &expiresAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, "", status.Error(codes.Unauthenticated, "invalid worker session")
		}
		return nil, "", err
	}
	if !expiresAt.After(now) {
		return nil, "", status.Error(codes.Unauthenticated, "worker session expired")
	}
	capabilities := &workerrolloutv1.WorkerCapabilities{}
	if err := protojson.Unmarshal(capabilitiesJSON, capabilities); err != nil {
		return nil, "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE rollout_worker_sessions SET last_heartbeat_at=$2,expires_at=$3 WHERE session_id=$1`, sessionID, now.UTC(), now.UTC().Add(rolloutkernel.DefaultWorkerSession)); err != nil {
		return nil, "", err
	}
	wireAPIs := make([]string, 0, len(capabilities.GetWireApis()))
	for _, wireAPI := range capabilities.GetWireApis() {
		wireAPIs = append(wireAPIs, wireAPI.String())
	}
	row := tx.QueryRow(ctx, `
		SELECT w.work_id,w.kind,COALESCE(w.rollout_id,''),COALESCE(w.episode_id,''),COALESCE(w.doctor_job_id,''),w.execution_generation,w.status,w.lease_token_hash,w.lease_expires_at,w.cancel_requested,w.result_digest,w.attempts,w.required_profile_id,w.required_profile_version,w.required_profile_concurrency,w.next_run_at,w.payload
	`+claimableWorkFromSQL+`
		ORDER BY w.next_run_at,w.work_id
		FOR UPDATE OF w SKIP LOCKED
		LIMIT 1
	`, now.UTC(), capabilities.GetPlanner(), capabilities.GetAgents(), workerID, capabilities.GetMaxConcurrency(), wireAPIs)
	work, err := scanWork(row)
	if err == pgx.ErrNoRows {
		claimResult = workClaimResultEmpty
		if err := tx.Commit(ctx); err != nil {
			return nil, "", err
		}
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	allowed, err := lockAndCheckWorkCapacity(ctx, tx, work, now)
	if err != nil {
		return nil, "", err
	}
	if !allowed {
		claimResult = workClaimResultCapacityBlocked
		if err := tx.Commit(ctx); err != nil {
			return nil, "", err
		}
		return nil, "", nil
	}
	if work.status == workStatusLeased {
		// The candidate lease is expired. Release only its uncommitted usage
		// allowance before issuing a replacement lease; committed usage remains
		// durable and is included in the rollout aggregate.
		if err := releaseUsageReservationsForWorkIDs(ctx, tx, []string{work.id}, now); err != nil {
			return nil, "", err
		}
	}
	leaseToken, err = randomToken()
	if err != nil {
		return nil, "", err
	}
	leaseExpires := now.UTC().Add(ttl)
	if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='LEASED',claim_owner=$2,lease_token_hash=$3,lease_expires_at=$4,attempts=attempts+1,updated_at=$1 WHERE work_id=$5`, now.UTC(), workerID, rolloutkernel.HashToken(leaseToken), leaseExpires, work.id); err != nil {
		return nil, "", err
	}
	switch work.kind {
	case "WORK_KIND_PROFILE_DOCTOR":
		if _, err := tx.Exec(ctx, `UPDATE agent_profile_doctor_jobs SET status='RUNNING' WHERE job_id=$1`, work.doctorJobID); err != nil {
			return nil, "", err
		}
	case "WORK_KIND_PLAN":
		if _, err := tx.Exec(ctx, `UPDATE rollouts SET status='ROLLOUT_STATUS_PLANNING',started_at=COALESCE(started_at,$2),version=version+1 WHERE rollout_id=$1`, work.rolloutID, now.UTC()); err != nil {
			return nil, "", err
		}
	case "WORK_KIND_EPISODE":
		if _, err := tx.Exec(ctx, `UPDATE rollout_episodes SET status='EPISODE_STATUS_LEASED',started_at=COALESCE(started_at,$2) WHERE episode_id=$1`, work.episodeID, now.UTC()); err != nil {
			return nil, "", err
		}
		if _, err := tx.Exec(ctx, `UPDATE rollouts SET status='ROLLOUT_STATUS_RUNNING',started_at=COALESCE(started_at,$2),version=version+1 WHERE rollout_id=$1 AND status='ROLLOUT_STATUS_QUEUED'`, work.rolloutID, now.UTC()); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", status.Error(codes.FailedPrecondition, "work item has unsupported kind")
	}
	item = &workerrolloutv1.WorkItem{ID: work.id, Kind: parseWorkKind(work.kind), RolloutID: work.rolloutID, EpisodeID: work.episodeID, ExecutionGeneration: work.generation, PayloadJson: work.payload, LeaseExpiresAt: timestamppb.New(leaseExpires)}
	switch work.kind {
	case "WORK_KIND_PROFILE_DOCTOR":
		var model string
		if err := tx.QueryRow(ctx, `SELECT model FROM agent_profile_doctor_jobs WHERE job_id=$1`, work.doctorJobID).Scan(&model); err != nil {
			return nil, "", err
		}
		item.ProfileDoctor = &workerrolloutv1.ProfileDoctorWork{JobID: work.doctorJobID, Model: model}
	case "WORK_KIND_PLAN", "WORK_KIND_EPISODE":
		rolloutRecord, err := scanRollout(tx.QueryRow(ctx, rolloutSelectSQL()+` WHERE rollout_id=$1`, work.rolloutID))
		if err != nil {
			return nil, "", err
		}
		item.Rollout = rolloutRecord.rollout
	}
	if work.episodeID != "" {
		episode, err := scanEpisode(tx.QueryRow(ctx, episodeSelectSQL()+` WHERE episode_id=$1`, work.episodeID))
		if err != nil {
			return nil, "", err
		}
		item.Episode = episode
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	claimResult = workClaimResultClaimed
	dueAt := work.nextRunAt
	if work.status == workStatusLeased && work.leaseExpiresAt != nil {
		dueAt = work.leaseExpiresAt.UTC()
	}
	recordRolloutWorkClaimLag(ctx, dueAt, now.UTC(), work.kind)
	return item, leaseToken, nil
}

// lockAndCheckWorkCapacity serializes admission for every provider-consuming
// work kind. The frozen Profile group includes planning probes, doctor probes,
// and episodes; rollout concurrency additionally applies to episodes.
func lockAndCheckWorkCapacity(ctx context.Context, tx pgx.Tx, work *workRecord, now time.Time) (bool, error) {
	if work.kind == "WORK_KIND_EPISODE" {
		var rolloutLimit, active int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(NULLIF((spec->'execution'->>'concurrency')::int,0),1) FROM rollouts WHERE rollout_id=$1 FOR UPDATE`, work.rolloutID).Scan(&rolloutLimit); err != nil {
			return false, err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM rollout_work_items WHERE rollout_id=$1 AND status='LEASED' AND lease_expires_at>$2`, work.rolloutID, now.UTC()).Scan(&active); err != nil {
			return false, err
		}
		if active >= rolloutLimit {
			return false, nil
		}
	}
	if work.requiredProfileID == "" {
		return true, nil
	}
	if work.requiredProfileVersion <= 0 || work.requiredProfileConcurrency <= 0 {
		return false, status.Error(codes.FailedPrecondition, "managed work has an invalid frozen profile concurrency contract")
	}
	profileGroup := fmt.Sprintf("%s:%d", work.requiredProfileID, work.requiredProfileVersion)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, profileGroup); err != nil {
		return false, err
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM rollout_work_items WHERE required_profile_id=$1 AND required_profile_version=$2 AND status='LEASED' AND lease_expires_at>$3`, work.requiredProfileID, work.requiredProfileVersion, now.UTC()).Scan(&active); err != nil {
		return false, err
	}
	return active < work.requiredProfileConcurrency, nil
}

func scanWork(row interface{ Scan(...any) error }) (*workRecord, error) {
	work := &workRecord{}
	if err := row.Scan(&work.id, &work.kind, &work.rolloutID, &work.episodeID, &work.doctorJobID, &work.generation, &work.status, &work.leaseTokenHash, &work.leaseExpiresAt, &work.cancelRequested, &work.resultDigest, &work.attempts, &work.requiredProfileID, &work.requiredProfileVersion, &work.requiredProfileConcurrency, &work.nextRunAt, &work.payload); err != nil {
		return nil, err
	}
	return work, nil
}

func parseWorkKind(value string) workerrolloutv1.WorkKind {
	if number, ok := workerrolloutv1.WorkKind_value[value]; ok {
		return workerrolloutv1.WorkKind(number)
	}
	return workerrolloutv1.WorkKind_WORK_KIND_UNSPECIFIED
}

func (s *Store) RenewWork(ctx context.Context, workID, leaseTokenHash string, now time.Time, ttl time.Duration) (time.Time, bool, error) {
	expiresAt := now.UTC().Add(ttl)
	var cancel bool
	err := s.db.Pool().QueryRow(ctx, `UPDATE rollout_work_items SET lease_expires_at=$3,updated_at=$2 WHERE work_id=$1 AND status='LEASED' AND lease_token_hash=$4 AND lease_expires_at>$2 RETURNING cancel_requested`, workID, now.UTC(), expiresAt, leaseTokenHash).Scan(&cancel)
	if err == pgx.ErrNoRows {
		return time.Time{}, false, status.Error(codes.FailedPrecondition, "work lease is not active")
	}
	return expiresAt, cancel, err
}

func (s *Store) ReportProgress(ctx context.Context, req *workerrolloutv1.ReportWorkProgressRequest, leaseTokenHash string, now time.Time) (bool, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockActiveWork(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET allocation_id=CASE WHEN $2='' THEN allocation_id ELSE $2 END,updated_at=$3 WHERE work_id=$1`, work.id, req.GetAllocationID(), now.UTC()); err != nil {
		return false, err
	}
	if work.rolloutID != "" {
		if err := insertEventTx(ctx, tx, work.rolloutID, work.episodeID, "work.progress", req.GetPhase(), req.GetMessage(), req.GetDetails(), now); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return work.cancelRequested, nil
}

func (s *Store) CompletePlan(ctx context.Context, req *workerrolloutv1.CompletePlanRequest, leaseTokenHash string, now time.Time) (*rolloutv1.Rollout, error) {
	if req == nil || req.GetResultDigest() == "" || req.GetSourceDigest() == "" || req.GetDescriptorDigest() == "" || len(req.GetTasks()) == 0 || req.GetPreflight() == nil {
		return nil, status.Error(codes.InvalidArgument, "complete plan requires digests, tasks, and a typed preflight report")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockWorkForCompletion(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return nil, err
	}
	if work.kind != "WORK_KIND_PLAN" {
		return nil, status.Error(codes.FailedPrecondition, "work item is not a plan")
	}
	if work.cancelRequested {
		return nil, status.Error(codes.Canceled, "planning work was cancelled")
	}
	if work.status == workStatusCompleted {
		if work.resultDigest != req.GetResultDigest() {
			return nil, status.Error(codes.AlreadyExists, "work completed with different result digest")
		}
		record, err := scanRollout(tx.QueryRow(ctx, rolloutSelectSQL()+` WHERE rollout_id=$1`, work.rolloutID))
		return record.rollout, err
	}
	var maxEpisodes int32
	var specJSON, frozenProfileJSON []byte
	var startPolicyText string
	if err := tx.QueryRow(ctx, `SELECT spec,start_policy,COALESCE(frozen_profile,'{}'::jsonb) FROM rollouts WHERE rollout_id=$1 FOR UPDATE`, work.rolloutID).Scan(&specJSON, &startPolicyText, &frozenProfileJSON); err != nil {
		return nil, err
	}
	spec := &rolloutv1.RolloutSpec{}
	if err := protojson.Unmarshal(specJSON, spec); err != nil {
		return nil, err
	}
	preflight := req.GetPreflight()
	if preflight == nil {
		return nil, status.Error(codes.InvalidArgument, "preflight report is required")
	}
	preflightUsage := preflight.GetUsage()
	if err := validateCommittedUsageTx(ctx, tx, work, req.GetUsageReservationID(), preflightUsage.GetInputTokens(), 0, preflightUsage.GetOutputTokens(), preflightUsage.GetCostMicrousd(), spec.GetAgent().GetProfile() != ""); err != nil {
		return nil, err
	}
	maxEpisodes = spec.GetBudget().GetMaxEpisodes()
	requiredWireAPI := ""
	if spec.GetAgent().GetProfile() != "" {
		frozenProfile := &agentprofilev1.AgentProfile{}
		if err := protojson.Unmarshal(frozenProfileJSON, frozenProfile); err != nil {
			return nil, fmt.Errorf("unmarshal frozen profile: %w", err)
		}
		requiredWireAPI = frozenProfile.GetSpec().GetWireApi().String()
	}
	attempts := spec.GetExecution().GetAttempts()
	total64 := int64(len(req.GetTasks())) * int64(attempts)
	if total64 > math.MaxInt32 {
		return nil, status.Error(codes.ResourceExhausted, "planned episode count exceeds supported maximum")
	}
	total := int32(total64)
	if total > rolloutkernel.MaxEpisodesPerRollout {
		return nil, status.Error(codes.ResourceExhausted, "planned episode count exceeds system limit")
	}
	if maxEpisodes > 0 && total > maxEpisodes {
		return nil, status.Error(codes.ResourceExhausted, "planned episodes exceed rollout budget")
	}
	planJSON := req.GetPlanJson()
	if len(planJSON) == 0 {
		planJSON = []byte(`{}`)
	}
	if !json.Valid(planJSON) {
		return nil, status.Error(codes.InvalidArgument, "plan_json must be JSON")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO rollout_plans(rollout_id,result_digest,plan,created_at) VALUES($1,$2,$3::jsonb,$4)`, work.rolloutID, req.GetResultDigest(), planJSON, now.UTC()); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for ordinal, task := range req.GetTasks() {
		if task.GetTaskID() == "" || task.GetTaskDigest() == "" {
			return nil, status.Error(codes.InvalidArgument, "planned task id and digest are required")
		}
		if _, ok := seen[task.GetTaskID()]; ok {
			return nil, status.Error(codes.InvalidArgument, "duplicate planned task id")
		}
		seen[task.GetTaskID()] = struct{}{}
		taskJSON := task.GetTaskJson()
		if len(taskJSON) == 0 {
			taskJSON = []byte(`{}`)
		}
		if !json.Valid(taskJSON) {
			return nil, status.Error(codes.InvalidArgument, "task_json must be JSON")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO rollout_tasks(rollout_id,task_id,task_digest,task,ordinal) VALUES($1,$2,$3,$4::jsonb,$5)`, work.rolloutID, task.GetTaskID(), task.GetTaskDigest(), taskJSON, ordinal); err != nil {
			return nil, err
		}
		for attempt := int32(1); attempt <= attempts; attempt++ {
			episodeID := "eps-" + uuid.NewString()
			if _, err := tx.Exec(ctx, `INSERT INTO rollout_episodes(episode_id,rollout_id,task_id,task_digest,attempt_index,execution_generation,status,created_at) VALUES($1,$2,$3,$4,$5,$6,'EPISODE_STATUS_PENDING',$7)`, episodeID, work.rolloutID, task.GetTaskID(), task.GetTaskDigest(), attempt, work.generation, now.UTC()); err != nil {
				return nil, err
			}
			workStatus := workStatusPending
			if startPolicyText == rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_MANUAL.String() {
				workStatus = "HELD"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO rollout_work_items(work_id,kind,rollout_id,episode_id,execution_generation,status,required_agent,required_wire_api,required_profile_id,required_profile_version,required_profile_concurrency,payload,next_run_at,created_at,updated_at) VALUES($1,'WORK_KIND_EPISODE',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$12,$12)`, "wrk-"+uuid.NewString(), work.rolloutID, episodeID, work.generation, workStatus, spec.GetAgent().GetName(), requiredWireAPI, work.requiredProfileID, work.requiredProfileVersion, work.requiredProfileConcurrency, taskJSON, now.UTC()); err != nil {
				return nil, err
			}
		}
	}
	preflightInput, preflightOutput, preflightCost, err := planningUsageTx(ctx, tx, work.rolloutID)
	if err != nil {
		return nil, err
	}
	summary := &rolloutv1.RolloutSummary{TaskCount: int32(len(req.GetTasks())), EpisodeCount: total, InputTokens: preflightInput, OutputTokens: preflightOutput, CostMicrousd: preflightCost}
	preflight.SourceDigest = req.GetSourceDigest()
	preflight.DescriptorDigest = req.GetDescriptorDigest()
	preflight.TaskCount = int32(len(req.GetTasks()))
	preflight.EpisodeCount = total
	summaryJSON, _ := protojson.Marshal(summary)
	preflightJSON, err := protojson.Marshal(preflight)
	if err != nil {
		return nil, fmt.Errorf("marshal preflight: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='COMPLETED',result_digest=$2,lease_expires_at=NULL,updated_at=$3 WHERE work_id=$1`, work.id, req.GetResultDigest(), now.UTC()); err != nil {
		return nil, err
	}
	rolloutStatus := rolloutv1.RolloutStatus_ROLLOUT_STATUS_QUEUED
	if startPolicyText == rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_MANUAL.String() {
		rolloutStatus = rolloutv1.RolloutStatus_ROLLOUT_STATUS_READY
	}
	if _, err := tx.Exec(ctx, `UPDATE rollouts SET status=$2,source_digest=$3,descriptor_digest=$4,summary=$5::jsonb,preflight=$6::jsonb,version=version+1 WHERE rollout_id=$1`, work.rolloutID, rolloutStatus.String(), req.GetSourceDigest(), req.GetDescriptorDigest(), summaryJSON, preflightJSON); err != nil {
		return nil, err
	}
	if err := insertEventTx(ctx, tx, work.rolloutID, "", "rollout.planned", "planning", "rollout plan frozen", map[string]string{"episodes": fmt.Sprint(total)}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	rollout, _, _, err := s.Get(ctx, work.rolloutID)
	return rollout, err
}

func (s *Store) CompleteEpisode(ctx context.Context, req *workerrolloutv1.CompleteEpisodeRequest, leaseTokenHash string, now time.Time) (*rolloutv1.Rollout, error) {
	if req == nil || req.GetEpisode() == nil || req.GetResultDigest() == "" {
		return nil, status.Error(codes.InvalidArgument, "episode and result_digest are required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockWorkForCompletion(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return nil, err
	}
	if work.kind != "WORK_KIND_EPISODE" || work.episodeID != req.GetEpisode().GetID() {
		return nil, status.Error(codes.FailedPrecondition, "episode does not match work item")
	}
	if work.status == workStatusCompleted {
		if work.resultDigest != req.GetResultDigest() {
			return nil, status.Error(codes.AlreadyExists, "work completed with different result digest")
		}
		record, err := scanRollout(tx.QueryRow(ctx, rolloutSelectSQL()+` WHERE rollout_id=$1`, work.rolloutID))
		return record.rollout, err
	}
	episode := req.GetEpisode()
	if episode.GetStatus() != rolloutv1.EpisodeStatus_EPISODE_STATUS_COMPLETED && episode.GetStatus() != rolloutv1.EpisodeStatus_EPISODE_STATUS_FAILED && episode.GetStatus() != rolloutv1.EpisodeStatus_EPISODE_STATUS_CANCELLED {
		return nil, status.Error(codes.InvalidArgument, "episode must be terminal")
	}
	var managed bool
	if err := tx.QueryRow(ctx, `SELECT profile_id IS NOT NULL FROM rollouts WHERE rollout_id=$1 FOR UPDATE`, work.rolloutID).Scan(&managed); err != nil {
		return nil, err
	}
	if err := validateCommittedUsageTx(ctx, tx, work, req.GetUsageReservationID(), episode.GetInputTokens(), episode.GetCachedInputTokens(), episode.GetOutputTokens(), episode.GetCostMicrousd(), managed); err != nil {
		return nil, err
	}
	var currentEpisodeStatus, currentFailureClass string
	if err := tx.QueryRow(ctx, `SELECT status,failure_class FROM rollout_episodes WHERE episode_id=$1 FOR UPDATE`, work.episodeID).Scan(&currentEpisodeStatus, &currentFailureClass); err != nil {
		return nil, err
	}
	if currentEpisodeStatus == rolloutv1.EpisodeStatus_EPISODE_STATUS_FAILED.String() && currentFailureClass == rolloutv1.FailureClass_FAILURE_CLASS_BUDGET.String() {
		episode.Status = rolloutv1.EpisodeStatus_EPISODE_STATUS_FAILED
		episode.FailureClass = rolloutv1.FailureClass_FAILURE_CLASS_BUDGET
		episode.Message = "wall-time budget exhausted"
	} else if work.cancelRequested {
		episode.Status = rolloutv1.EpisodeStatus_EPISODE_STATUS_CANCELLED
		episode.FailureClass = rolloutv1.FailureClass_FAILURE_CLASS_UNSPECIFIED
		episode.Message = "cancelled"
	}
	factsJSON := []byte(`{}`)
	if episode.GetExecutionFacts() != nil {
		var err error
		factsJSON, err = protojson.Marshal(episode.GetExecutionFacts())
		if err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE rollout_episodes SET status=$2,failure_class=$3,passed=$4,reward=$5,input_tokens=$6,cached_input_tokens=$7,output_tokens=$8,cost_microusd=$9,duration_ms=$10,execution_facts=$11::jsonb,artifact_manifest_id=$12,message=$13,completed_at=$14,started_at=COALESCE(started_at,$14) WHERE episode_id=$1 AND execution_generation=$15`, work.episodeID, episode.GetStatus().String(), episode.GetFailureClass().String(), episode.GetPassed(), episode.GetReward(), episode.GetInputTokens(), episode.GetCachedInputTokens(), episode.GetOutputTokens(), episode.GetCostMicrousd(), episode.GetDurationMs(), factsJSON, episode.GetArtifactManifestID(), episode.GetMessage(), now.UTC(), work.generation); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='COMPLETED',result_digest=$2,lease_expires_at=NULL,updated_at=$3 WHERE work_id=$1`, work.id, req.GetResultDigest(), now.UTC()); err != nil {
		return nil, err
	}
	if err := refreshRolloutTx(ctx, tx, work.rolloutID, now); err != nil {
		return nil, err
	}
	if err := insertEventTx(ctx, tx, work.rolloutID, work.episodeID, "episode.completed", "collecting", episode.GetMessage(), nil, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	rollout, _, _, err := s.Get(ctx, work.rolloutID)
	return rollout, err
}

func (s *Store) CompleteProfileDoctor(ctx context.Context, req *workerrolloutv1.CompleteProfileDoctorRequest, leaseTokenHash string, now time.Time) error {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockActiveWork(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return err
	}
	if work.kind != "WORK_KIND_PROFILE_DOCTOR" {
		return status.Error(codes.FailedPrecondition, "work item is not a profile doctor")
	}
	var profileJSON []byte
	if err := tx.QueryRow(ctx, `SELECT frozen_profile FROM agent_profile_doctor_jobs WHERE job_id=$1 FOR UPDATE`, work.doctorJobID).Scan(&profileJSON); err != nil {
		return err
	}
	profile := &agentprofilev1.AgentProfile{}
	if err := protojson.Unmarshal(profileJSON, profile); err != nil {
		return err
	}
	responseJSON, err := protojson.Marshal(&agentprofilev1.DoctorAgentProfileResponse{Profile: profile, Checks: req.GetChecks(), Healthy: req.GetHealthy()})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_profile_doctor_jobs SET status='COMPLETED',checks=$2::jsonb,healthy=$3,completed_at=$4 WHERE job_id=$1`, work.doctorJobID, responseJSON, req.GetHealthy(), now.UTC()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='COMPLETED',lease_token_hash='',lease_expires_at=NULL,updated_at=$2 WHERE work_id=$1`, work.id, now.UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) FailWork(ctx context.Context, req *workerrolloutv1.FailWorkRequest, leaseTokenHash string, now time.Time) (*rolloutv1.Rollout, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockActiveWork(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return nil, err
	}
	if work.kind == "WORK_KIND_PROFILE_DOCTOR" {
		if req.GetRetriable() && work.attempts <= rolloutkernel.DefaultInfrastructureRetry {
			delay := 5 * time.Second
			if work.attempts > 1 {
				delay = 30 * time.Second
			}
			if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='PENDING',claim_owner='',lease_token_hash='',lease_expires_at=NULL,next_run_at=$2,last_error=$3,updated_at=$4 WHERE work_id=$1`, work.id, now.UTC().Add(delay), req.GetMessage(), now.UTC()); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE agent_profile_doctor_jobs SET status='PENDING',error='' WHERE job_id=$1`, work.doctorJobID); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return nil, nil
		}
		if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='FAILED',last_error=$2,lease_token_hash='',lease_expires_at=NULL,updated_at=$3 WHERE work_id=$1`, work.id, req.GetMessage(), now.UTC()); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE agent_profile_doctor_jobs SET status='FAILED',error=$2,completed_at=$3 WHERE job_id=$1`, work.doctorJobID, req.GetMessage(), now.UTC()); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if work.cancelRequested {
		if err := releaseUsageReservationsForWorkIDs(ctx, tx, []string{work.id}, now); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='CANCELLED',lease_expires_at=NULL,updated_at=$2 WHERE work_id=$1`, work.id, now.UTC()); err != nil {
			return nil, err
		}
		if work.episodeID != "" {
			if _, err := tx.Exec(ctx, `UPDATE rollout_episodes SET status='EPISODE_STATUS_CANCELLED',message='cancelled',completed_at=$2 WHERE episode_id=$1 AND failure_class<>'FAILURE_CLASS_BUDGET'`, work.episodeID, now.UTC()); err != nil {
				return nil, err
			}
			if err := refreshRolloutTx(ctx, tx, work.rolloutID, now); err != nil {
				return nil, err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE rollouts SET status='ROLLOUT_STATUS_CANCELLED',message='cancelled',completed_at=$2,version=version+1 WHERE rollout_id=$1`, work.rolloutID, now.UTC()); err != nil {
				return nil, err
			}
		}
		if err := insertEventTx(ctx, tx, work.rolloutID, work.episodeID, "work.cancelled", "control", "work acknowledged cancellation", nil, now); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		rollout, _, _, err := s.Get(ctx, work.rolloutID)
		return rollout, err
	}
	if req.GetRetriable() && work.attempts <= rolloutkernel.DefaultInfrastructureRetry {
		delay := 5 * time.Second
		if work.attempts > 1 {
			delay = 30 * time.Second
		}
		if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='PENDING',claim_owner='',lease_token_hash='',lease_expires_at=NULL,next_run_at=$2,last_error=$3,updated_at=$4 WHERE work_id=$1`, work.id, now.UTC().Add(delay), req.GetMessage(), now.UTC()); err != nil {
			return nil, err
		}
		if err := insertEventTx(ctx, tx, work.rolloutID, work.episodeID, "work.retrying", "infrastructure", req.GetMessage(), map[string]string{"code": req.GetCode()}, now); err != nil {
			return nil, err
		}
	} else {
		if err := releaseUsageReservationsForWorkIDs(ctx, tx, []string{work.id}, now); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='FAILED',last_error=$2,lease_token_hash='',lease_expires_at=NULL,updated_at=$3 WHERE work_id=$1`, work.id, req.GetMessage(), now.UTC()); err != nil {
			return nil, err
		}
		if work.episodeID != "" {
			failureClass := failureClassForWorkCode(req.GetCode()).String()
			if _, err := tx.Exec(ctx, `UPDATE rollout_episodes SET status='EPISODE_STATUS_FAILED',failure_class=$2,message=$3,completed_at=$4 WHERE episode_id=$1`, work.episodeID, failureClass, req.GetMessage(), now.UTC()); err != nil {
				return nil, err
			}
		} else {
			failureClass := failureClassForWorkCode(req.GetCode()).String()
			preflightJSON := []byte(nil)
			if req.GetPreflight() != nil {
				preflightJSON, err = protojson.Marshal(req.GetPreflight())
				if err != nil {
					return nil, err
				}
			}
			inputTokens, outputTokens, costMicrousd, err := planningUsageTx(ctx, tx, work.rolloutID)
			if err != nil {
				return nil, err
			}
			summaryJSON, err := protojson.Marshal(&rolloutv1.RolloutSummary{InputTokens: inputTokens, OutputTokens: outputTokens, CostMicrousd: costMicrousd})
			if err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE rollouts SET status='ROLLOUT_STATUS_FAILED',failure_class=$2,message=$3,completed_at=$4,preflight=COALESCE($5::jsonb,preflight),summary=$6::jsonb,version=version+1 WHERE rollout_id=$1`, work.rolloutID, failureClass, req.GetMessage(), now.UTC(), preflightJSON, summaryJSON); err != nil {
				return nil, err
			}
		}
		if work.episodeID != "" {
			if err := refreshRolloutTx(ctx, tx, work.rolloutID, now); err != nil {
				return nil, err
			}
		}
		phase := "infrastructure"
		if work.kind == "WORK_KIND_PLAN" {
			phase = "planning"
		}
		if err := insertEventTx(ctx, tx, work.rolloutID, work.episodeID, "work.failed", phase, req.GetMessage(), map[string]string{"code": req.GetCode()}, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	rollout, _, _, err := s.Get(ctx, work.rolloutID)
	return rollout, err
}

func failureClassForWorkCode(code string) rolloutv1.FailureClass {
	switch code {
	case "BUDGET_EXHAUSTED":
		return rolloutv1.FailureClass_FAILURE_CLASS_BUDGET
	case "METERING_FAILED":
		return rolloutv1.FailureClass_FAILURE_CLASS_METERING
	case "PREFLIGHT_REJECTED":
		return rolloutv1.FailureClass_FAILURE_CLASS_UNSPECIFIED
	default:
		return rolloutv1.FailureClass_FAILURE_CLASS_INFRASTRUCTURE
	}
}

// ReconcileExpiredLeases converges cancellation even when the worker holding a
// lease disappears before acknowledging it. Budget-expired rollouts remain
// failed; only an explicit CANCELLING rollout transitions to CANCELLED here.
func (s *Store) ReconcileExpiredLeases(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Pool().Query(ctx, `SELECT DISTINCT rollout_id FROM rollout_work_items WHERE status='LEASED' AND cancel_requested=TRUE AND lease_expires_at<=$1 ORDER BY rollout_id LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return 0, err
	}
	var rolloutIDs []string
	for rows.Next() {
		var rolloutID string
		if err := rows.Scan(&rolloutID); err != nil {
			rows.Close()
			return 0, err
		}
		rolloutIDs = append(rolloutIDs, rolloutID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	converged := 0
	for _, rolloutID := range rolloutIDs {
		tx, err := s.db.Pool().Begin(ctx)
		if err != nil {
			return converged, err
		}
		var workIDs, episodeIDs []string
		workRows, err := tx.Query(ctx, `SELECT work_id,COALESCE(episode_id,'') FROM rollout_work_items WHERE rollout_id=$1 AND status='LEASED' AND cancel_requested=TRUE AND lease_expires_at<=$2 ORDER BY work_id FOR UPDATE`, rolloutID, now.UTC())
		if err != nil {
			_ = tx.Rollback(ctx)
			return converged, err
		}
		for workRows.Next() {
			var workID, episodeID string
			if err := workRows.Scan(&workID, &episodeID); err != nil {
				workRows.Close()
				_ = tx.Rollback(ctx)
				return converged, err
			}
			workIDs = append(workIDs, workID)
			if episodeID != "" {
				episodeIDs = append(episodeIDs, episodeID)
			}
		}
		if err := workRows.Err(); err != nil {
			workRows.Close()
			_ = tx.Rollback(ctx)
			return converged, err
		}
		workRows.Close()
		if len(workIDs) == 0 {
			_ = tx.Rollback(ctx)
			continue
		}
		if err := releaseUsageReservationsForWorkIDs(ctx, tx, workIDs, now); err != nil {
			_ = tx.Rollback(ctx)
			return converged, err
		}
		if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='CANCELLED',lease_token_hash='',lease_expires_at=NULL,updated_at=$2 WHERE work_id=ANY($1)`, workIDs, now.UTC()); err != nil {
			_ = tx.Rollback(ctx)
			return converged, err
		}
		if len(episodeIDs) > 0 {
			if _, err := tx.Exec(ctx, `UPDATE rollout_episodes SET status='EPISODE_STATUS_CANCELLED',message='cancelled after worker lease expired',completed_at=$2 WHERE episode_id=ANY($1) AND failure_class<>'FAILURE_CLASS_BUDGET'`, episodeIDs, now.UTC()); err != nil {
				_ = tx.Rollback(ctx)
				return converged, err
			}
		}
		var rolloutStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM rollouts WHERE rollout_id=$1 FOR UPDATE`, rolloutID).Scan(&rolloutStatus); err != nil {
			_ = tx.Rollback(ctx)
			return converged, err
		}
		if rolloutStatus == rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLING.String() {
			var active int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM rollout_work_items WHERE rollout_id=$1 AND status IN('PENDING','LEASED')`, rolloutID).Scan(&active); err != nil {
				_ = tx.Rollback(ctx)
				return converged, err
			}
			if active == 0 {
				if _, err := tx.Exec(ctx, `UPDATE rollouts SET status='ROLLOUT_STATUS_CANCELLED',message='cancelled',completed_at=$2,version=version+1 WHERE rollout_id=$1`, rolloutID, now.UTC()); err != nil {
					_ = tx.Rollback(ctx)
					return converged, err
				}
			}
		}
		if err := insertEventTx(ctx, tx, rolloutID, "", "work.lease_expired", "control", "cancelled work after worker lease expired", map[string]string{"work_items": fmt.Sprint(len(workIDs))}, now); err != nil {
			_ = tx.Rollback(ctx)
			return converged, err
		}
		if err := tx.Commit(ctx); err != nil {
			return converged, err
		}
		converged += len(workIDs)
	}
	return converged, nil
}

// releaseUsageReservationsForWorkIDs closes server-owned budget reservations
// before the corresponding work loses its active lease. Client-side cleanup is
// best effort and cannot authorize a release after a terminal transition.
func releaseUsageReservationsForWorkIDs(ctx context.Context, tx pgx.Tx, workIDs []string, now time.Time) error {
	if len(workIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE rollout_usage_reservations r SET status='RELEASED',completed_at=$2 FROM rollout_work_items w WHERE w.work_id=ANY($1) AND r.rollout_id=w.rollout_id AND r.episode_id IS NOT DISTINCT FROM w.episode_id AND r.execution_generation=w.execution_generation AND r.status='RESERVED'`, workIDs, now.UTC())
	return err
}

func planningUsageTx(ctx context.Context, tx pgx.Tx, rolloutID string) (inputTokens, outputTokens, costMicrousd int64, err error) {
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(actual_input_tokens),0),COALESCE(sum(actual_output_tokens),0),COALESCE(sum(actual_cost_microusd),0)
		FROM rollout_usage_reservations WHERE rollout_id=$1 AND episode_id IS NULL AND status='COMMITTED'`, rolloutID).Scan(&inputTokens, &outputTokens, &costMicrousd)
	return
}

func lockActiveWork(ctx context.Context, tx pgx.Tx, workID, tokenHash string, now time.Time) (*workRecord, error) {
	work, err := scanWork(tx.QueryRow(ctx, `SELECT work_id,kind,COALESCE(rollout_id,''),COALESCE(episode_id,''),COALESCE(doctor_job_id,''),execution_generation,status,lease_token_hash,lease_expires_at,cancel_requested,result_digest,attempts,required_profile_id,required_profile_version,required_profile_concurrency,next_run_at,payload FROM rollout_work_items WHERE work_id=$1 FOR UPDATE`, workID))
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, "work item not found")
	}
	if err != nil {
		return nil, err
	}
	if work.status != workStatusLeased || work.leaseTokenHash != tokenHash || work.leaseExpiresAt == nil || !work.leaseExpiresAt.After(now) {
		return nil, status.Error(codes.FailedPrecondition, "work lease is not active")
	}
	return work, nil
}

func lockWorkForCompletion(ctx context.Context, tx pgx.Tx, workID, tokenHash string, now time.Time) (*workRecord, error) {
	work, err := scanWork(tx.QueryRow(ctx, `SELECT work_id,kind,COALESCE(rollout_id,''),COALESCE(episode_id,''),COALESCE(doctor_job_id,''),execution_generation,status,lease_token_hash,lease_expires_at,cancel_requested,result_digest,attempts,required_profile_id,required_profile_version,required_profile_concurrency,next_run_at,payload FROM rollout_work_items WHERE work_id=$1 FOR UPDATE`, workID))
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, "work item not found")
	}
	if err != nil {
		return nil, err
	}
	if work.status == workStatusCompleted && work.leaseTokenHash == tokenHash {
		return work, nil
	}
	if work.status != workStatusLeased || work.leaseTokenHash != tokenHash || work.leaseExpiresAt == nil || !work.leaseExpiresAt.After(now) {
		return nil, status.Error(codes.FailedPrecondition, "work lease is not active")
	}
	return work, nil
}

func refreshRolloutTx(ctx context.Context, tx pgx.Tx, rolloutID string, now time.Time) error {
	var summaryJSON []byte
	err := tx.QueryRow(ctx, `SELECT jsonb_build_object(
		'taskCount',(SELECT count(*) FROM rollout_tasks WHERE rollout_id=$1),
		'episodeCount',count(*),
		'completedEpisodes',count(*) FILTER(WHERE status='EPISODE_STATUS_COMPLETED'),
		'failedEpisodes',count(*) FILTER(WHERE status='EPISODE_STATUS_FAILED'),
		'cancelledEpisodes',count(*) FILTER(WHERE status='EPISODE_STATUS_CANCELLED'),
		'passedEpisodes',count(*) FILTER(WHERE passed),
		'inputTokens',COALESCE(sum(CASE WHEN NOT EXISTS (SELECT 1 FROM rollout_usage_reservations r WHERE r.rollout_id=$1 AND r.episode_id=rollout_episodes.episode_id AND r.status='COMMITTED') THEN input_tokens ELSE 0 END),0)+(SELECT COALESCE(sum(actual_input_tokens),0) FROM rollout_usage_reservations WHERE rollout_id=$1 AND status='COMMITTED'),
		'cachedInputTokens',COALESCE(sum(CASE WHEN NOT EXISTS (SELECT 1 FROM rollout_usage_reservations r WHERE r.rollout_id=$1 AND r.episode_id=rollout_episodes.episode_id AND r.status='COMMITTED') THEN cached_input_tokens ELSE 0 END),0)+(SELECT COALESCE(sum(actual_cached_input_tokens),0) FROM rollout_usage_reservations WHERE rollout_id=$1 AND status='COMMITTED'),
		'outputTokens',COALESCE(sum(CASE WHEN NOT EXISTS (SELECT 1 FROM rollout_usage_reservations r WHERE r.rollout_id=$1 AND r.episode_id=rollout_episodes.episode_id AND r.status='COMMITTED') THEN output_tokens ELSE 0 END),0)+(SELECT COALESCE(sum(actual_output_tokens),0) FROM rollout_usage_reservations WHERE rollout_id=$1 AND status='COMMITTED'),
		'costMicrousd',COALESCE(sum(CASE WHEN NOT EXISTS (SELECT 1 FROM rollout_usage_reservations r WHERE r.rollout_id=$1 AND r.episode_id=rollout_episodes.episode_id AND r.status='COMMITTED') THEN cost_microusd ELSE 0 END),0)+(SELECT COALESCE(sum(actual_cost_microusd),0) FROM rollout_usage_reservations WHERE rollout_id=$1 AND status='COMMITTED'),
		'totalDurationMs',COALESCE(sum(duration_ms),0))
		FROM rollout_episodes WHERE rollout_id=$1`, rolloutID).Scan(&summaryJSON)
	if err != nil {
		return err
	}
	var active int
	var failed int
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status NOT IN ('EPISODE_STATUS_COMPLETED','EPISODE_STATUS_FAILED','EPISODE_STATUS_CANCELLED')),count(*) FILTER(WHERE status='EPISODE_STATUS_FAILED') FROM rollout_episodes WHERE rollout_id=$1`, rolloutID).Scan(&active, &failed); err != nil {
		return err
	}
	statusText := "ROLLOUT_STATUS_RUNNING"
	var completed any
	var currentStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM rollouts WHERE rollout_id=$1 FOR UPDATE`, rolloutID).Scan(&currentStatus); err != nil {
		return err
	}
	if currentStatus == "ROLLOUT_STATUS_CANCELLING" {
		statusText = currentStatus
	}
	if active == 0 {
		completed = now.UTC()
		if currentStatus == "ROLLOUT_STATUS_CANCELLING" {
			statusText = "ROLLOUT_STATUS_CANCELLED"
		} else if failed > 0 {
			statusText = "ROLLOUT_STATUS_FAILED"
		} else {
			statusText = "ROLLOUT_STATUS_COMPLETED"
		}
	}
	var rolloutFailureClass string
	if err := tx.QueryRow(ctx, `SELECT CASE
		WHEN bool_or(failure_class='FAILURE_CLASS_BUDGET') THEN 'FAILURE_CLASS_BUDGET'
		WHEN bool_or(failure_class='FAILURE_CLASS_METERING') THEN 'FAILURE_CLASS_METERING'
		WHEN bool_or(failure_class='FAILURE_CLASS_INFRASTRUCTURE') THEN 'FAILURE_CLASS_INFRASTRUCTURE'
		WHEN bool_or(failure_class='FAILURE_CLASS_AGENT') THEN 'FAILURE_CLASS_AGENT'
		WHEN bool_or(failure_class='FAILURE_CLASS_VERIFIER') THEN 'FAILURE_CLASS_VERIFIER'
		ELSE 'FAILURE_CLASS_UNSPECIFIED' END
		FROM rollout_episodes WHERE rollout_id=$1`, rolloutID).Scan(&rolloutFailureClass); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE rollouts SET status=$2,failure_class=$3,summary=$4::jsonb,started_at=COALESCE(started_at,$5),completed_at=$6,version=version+1 WHERE rollout_id=$1`, rolloutID, statusText, rolloutFailureClass, summaryJSON, now.UTC(), completed)
	return err
}
