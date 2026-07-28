package pgrollout

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type idempotentRollout struct {
	rollout  *rolloutv1.Rollout
	specHash string
}

func getRolloutByIdempotencyTx(ctx context.Context, tx pgx.Tx, namespace, key string) (*idempotentRollout, error) {
	record, err := scanRollout(tx.QueryRow(ctx, rolloutSelectSQL()+` WHERE namespace = $1 AND idempotency_key = $2`, strings.TrimSpace(namespace), strings.TrimSpace(key)))
	if err != nil {
		return nil, fmt.Errorf("lookup rollout by idempotency key: %w", err)
	}
	return &idempotentRollout{rollout: record.rollout, specHash: record.specHash}, nil
}

func (s *Store) Get(ctx context.Context, id string) (*rolloutv1.Rollout, []*rolloutv1.Episode, bool, error) {
	record, err := scanRollout(s.db.Pool().QueryRow(ctx, rolloutSelectSQL()+` WHERE rollout_id = $1`, strings.TrimSpace(id)))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("get rollout: %w", err)
	}
	rows, err := s.db.Pool().Query(ctx, episodeSelectSQL()+` WHERE rollout_id = $1 ORDER BY task_id, attempt_index`, id)
	if err != nil {
		return nil, nil, false, fmt.Errorf("list rollout episodes: %w", err)
	}
	defer rows.Close()
	var episodes []*rolloutv1.Episode
	for rows.Next() {
		episode, scanErr := scanEpisode(rows)
		if scanErr != nil {
			return nil, nil, false, scanErr
		}
		episodes = append(episodes, episode)
	}
	return record.rollout, episodes, true, rows.Err()
}

func (s *Store) List(ctx context.Context, filter *rolloutv1.RolloutListFilter) ([]*rolloutv1.Rollout, string, error) {
	if filter == nil || strings.TrimSpace(filter.GetNamespace()) == "" {
		return nil, "", status.Error(codes.InvalidArgument, "filter.namespace is required")
	}
	pageSize := int(filter.GetPageSize())
	if pageSize <= 0 {
		pageSize = rolloutkernel.DefaultPageSize
	}
	if pageSize > rolloutkernel.MaxPageSize {
		return nil, "", status.Error(codes.InvalidArgument, "page_size exceeds maximum")
	}
	args := []any{strings.TrimSpace(filter.GetNamespace())}
	where := []string{"namespace = $1"}
	if len(filter.GetStatuses()) > 0 {
		values := make([]string, 0, len(filter.GetStatuses()))
		for _, value := range filter.GetStatuses() {
			values = append(values, value.String())
		}
		args = append(args, values)
		where = append(where, fmt.Sprintf("status = ANY($%d)", len(args)))
	}
	if value := strings.TrimSpace(filter.GetTaskSetDigest()); value != "" {
		args = append(args, value)
		where = append(where, fmt.Sprintf("(descriptor_digest = $%d OR spec->>'taskSetRef' = $%d)", len(args), len(args)))
	}
	if value := strings.TrimSpace(filter.GetAgent()); value != "" {
		args = append(args, value)
		where = append(where, fmt.Sprintf("spec->'agent'->>'name' = $%d", len(args)))
	}
	if value := strings.TrimSpace(filter.GetModel()); value != "" {
		args = append(args, value)
		where = append(where, fmt.Sprintf("spec->>'model' = $%d", len(args)))
	}
	if len(filter.GetLabels()) > 0 {
		encoded, _ := json.Marshal(filter.GetLabels())
		args = append(args, encoded)
		where = append(where, fmt.Sprintf("labels @> $%d::jsonb", len(args)))
	}
	if filter.GetCursor() != "" {
		createdAt, id, err := decodeCursor(filter.GetCursor())
		if err != nil {
			return nil, "", status.Error(codes.InvalidArgument, "invalid cursor")
		}
		args = append(args, createdAt, id)
		where = append(where, fmt.Sprintf("(created_at, rollout_id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, pageSize+1)
	query := rolloutSelectSQL() + ` WHERE ` + strings.Join(where, " AND ") + fmt.Sprintf(` ORDER BY created_at DESC, rollout_id DESC LIMIT $%d`, len(args))
	rows, err := s.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list rollouts: %w", err)
	}
	defer rows.Close()
	rollouts := make([]*rolloutv1.Rollout, 0, pageSize+1)
	for rows.Next() {
		record, scanErr := scanRollout(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		rollouts = append(rollouts, record.rollout)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(rollouts) > pageSize {
		rollouts = rollouts[:pageSize]
		last := rollouts[len(rollouts)-1]
		next = encodeCursor(last.GetCreatedAt().AsTime(), last.GetID())
	}
	return rollouts, next, nil
}

func encodeCursor(createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(createdAt.UnixNano(), 10) + "\x00" + id))
}

func decodeCursor(value string) (time.Time, string, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, "", err
	}
	parts := strings.SplitN(string(data), "\x00", 2)
	if len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", fmt.Errorf("malformed cursor")
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	return time.Unix(0, nanos).UTC(), parts[1], err
}

func (s *Store) Cancel(ctx context.Context, id string, now time.Time) (*rolloutv1.Rollout, bool, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT work_id,status FROM rollout_work_items WHERE rollout_id=$1 AND status IN ('HELD','PENDING','LEASED') ORDER BY work_id FOR UPDATE`, id)
	if err != nil {
		return nil, false, err
	}
	var pendingWorkIDs []string
	for rows.Next() {
		var workID, workStatus string
		if err := rows.Scan(&workID, &workStatus); err != nil {
			rows.Close()
			return nil, false, err
		}
		if workStatus == workStatusPending || workStatus == "HELD" {
			pendingWorkIDs = append(pendingWorkIDs, workID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, false, err
	}
	rows.Close()
	record, err := scanRollout(tx.QueryRow(ctx, rolloutSelectSQL()+` WHERE rollout_id = $1 FOR UPDATE`, id))
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if rolloutkernel.IsTerminal(record.rollout.GetStatus()) {
		return record.rollout, true, nil
	}
	if err := releaseUsageReservationsForWorkIDs(ctx, tx, pendingWorkIDs, now); err != nil {
		return nil, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status = CASE WHEN status IN ('HELD','PENDING') THEN 'CANCELLED' ELSE status END, cancel_requested = CASE WHEN status = 'LEASED' THEN TRUE ELSE cancel_requested END, updated_at = $2 WHERE rollout_id = $1 AND status IN ('HELD','PENDING','LEASED')`, id, now.UTC()); err != nil {
		return nil, false, err
	}
	var leased int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM rollout_work_items WHERE rollout_id=$1 AND status='LEASED'`, id).Scan(&leased); err != nil {
		return nil, false, err
	}
	newStatus := rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLING
	var completed any
	if leased == 0 {
		newStatus = rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLED
		completed = now.UTC()
		if _, err := tx.Exec(ctx, `UPDATE rollout_episodes SET status=$2, completed_at=$3 WHERE rollout_id=$1 AND status NOT IN ('EPISODE_STATUS_COMPLETED','EPISODE_STATUS_FAILED','EPISODE_STATUS_CANCELLED')`, id, rolloutv1.EpisodeStatus_EPISODE_STATUS_CANCELLED.String(), now.UTC()); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE rollouts SET status=$2, message='cancel requested', completed_at=$3, version=version+1 WHERE rollout_id=$1`, id, newStatus.String(), completed); err != nil {
		return nil, false, err
	}
	if err := insertEventTx(ctx, tx, id, "", "rollout.cancel_requested", "control", "cancel requested", nil, now); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	updated, _, ok, err := s.Get(ctx, id)
	return updated, ok, err
}

func (s *Store) Start(ctx context.Context, id, idempotencyKey string, now time.Time) (*rolloutv1.Rollout, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, false, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanRollout(tx.QueryRow(ctx, rolloutSelectSQL()+` WHERE rollout_id=$1 FOR UPDATE`, strings.TrimSpace(id)))
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var storedKey *string
	if err := tx.QueryRow(ctx, `SELECT start_idempotency_key FROM rollouts WHERE rollout_id=$1`, strings.TrimSpace(id)).Scan(&storedKey); err != nil {
		return nil, false, err
	}
	statusValue := record.rollout.GetStatus()
	if statusValue != rolloutv1.RolloutStatus_ROLLOUT_STATUS_READY {
		// Once a rollout has crossed READY, starting it again is a read of the
		// durable result of the first start. The caller may legitimately use a
		// newly generated key in a later CLI invocation.
		if storedKey != nil {
			return record.rollout, true, nil
		}
		return nil, true, status.Error(codes.FailedPrecondition, "rollout is not ready")
	}
	result, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='PENDING',updated_at=$2 WHERE rollout_id=$1 AND kind='WORK_KIND_EPISODE' AND status='HELD'`, id, now.UTC())
	if err != nil {
		return nil, false, err
	}
	if result.RowsAffected() == 0 {
		return nil, true, status.Error(codes.FailedPrecondition, "ready rollout has no held episode work")
	}
	if _, err := tx.Exec(ctx, `UPDATE rollouts SET status='ROLLOUT_STATUS_QUEUED',start_idempotency_key=$2,started_at=COALESCE(started_at,$3),version=version+1 WHERE rollout_id=$1`, id, idempotencyKey, now.UTC()); err != nil {
		return nil, false, err
	}
	if err := insertEventTx(ctx, tx, id, "", "rollout.started", "control", "rollout started", nil, now); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	updated, _, ok, err := s.Get(ctx, id)
	return updated, ok, err
}

func (s *Store) Retry(ctx context.Context, id string, now time.Time) (*rolloutv1.Rollout, bool, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanRollout(tx.QueryRow(ctx, rolloutSelectSQL()+` WHERE rollout_id=$1 FOR UPDATE`, id))
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if record.rollout.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED {
		return nil, true, status.Error(codes.FailedPrecondition, "only failed rollouts can be retried")
	}
	generation := record.rollout.GetVersion() + 1
	var planExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM rollout_plans WHERE rollout_id=$1)`, id).Scan(&planExists); err != nil {
		return nil, false, err
	}
	queued := int64(0)
	requiredWireAPI := ""
	requiredProfileID := ""
	var requiredProfileVersion int64
	var requiredProfileConcurrency int32
	if record.rollout.GetSpec().GetAgent().GetProfile() != "" {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(profile_id,''),COALESCE((frozen_profile->>'version')::bigint,0),COALESCE((frozen_profile->'spec'->>'maxConcurrency')::int,0),COALESCE(frozen_profile->'spec'->>'wireApi','') FROM rollouts WHERE rollout_id=$1`, id).Scan(&requiredProfileID, &requiredProfileVersion, &requiredProfileConcurrency, &requiredWireAPI); err != nil {
			return nil, false, err
		}
	}
	if !planExists {
		result, execErr := tx.Exec(ctx, `INSERT INTO rollout_work_items(work_id,kind,rollout_id,execution_generation,status,required_wire_api,required_profile_id,required_profile_version,required_profile_concurrency,payload,next_run_at,created_at,updated_at) VALUES($1,'WORK_KIND_PLAN',$2,$3,'PENDING',$4,$5,$6,$7,'{}',$8,$8,$8)`, "wrk-"+uuid.NewString(), id, generation, requiredWireAPI, requiredProfileID, requiredProfileVersion, requiredProfileConcurrency, now.UTC())
		if execErr != nil {
			return nil, false, execErr
		}
		queued = result.RowsAffected()
	} else {
		type retryEpisode struct {
			id      string
			payload []byte
		}
		rows, queryErr := tx.Query(ctx, `SELECT e.episode_id,t.task FROM rollout_episodes e JOIN rollout_tasks t ON t.rollout_id=e.rollout_id AND t.task_id=e.task_id WHERE e.rollout_id=$1 AND e.status='EPISODE_STATUS_FAILED' AND e.failure_class='FAILURE_CLASS_INFRASTRUCTURE' ORDER BY e.episode_id FOR UPDATE OF e`, id)
		if queryErr != nil {
			return nil, false, queryErr
		}
		var episodes []retryEpisode
		for rows.Next() {
			var episode retryEpisode
			if err := rows.Scan(&episode.id, &episode.payload); err != nil {
				rows.Close()
				return nil, false, err
			}
			episodes = append(episodes, episode)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, false, err
		}
		rows.Close()
		requiredAgent := record.rollout.GetSpec().GetAgent().GetName()
		for _, episode := range episodes {
			if _, err := tx.Exec(ctx, `UPDATE rollout_episodes SET status='EPISODE_STATUS_PENDING',failure_class='FAILURE_CLASS_UNSPECIFIED',execution_generation=$2,passed=FALSE,reward=0,input_tokens=0,cached_input_tokens=0,output_tokens=0,cost_microusd=0,duration_ms=0,execution_facts='{}'::jsonb,artifact_manifest_id='',message='',started_at=NULL,completed_at=NULL WHERE episode_id=$1`, episode.id, generation); err != nil {
				return nil, false, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO rollout_work_items(work_id,kind,rollout_id,episode_id,execution_generation,status,required_agent,required_wire_api,required_profile_id,required_profile_version,required_profile_concurrency,payload,next_run_at,created_at,updated_at) VALUES($1,'WORK_KIND_EPISODE',$2,$3,$4,'PENDING',$5,$6,$7,$8,$9,$10::jsonb,$11,$11,$11)`, "wrk-"+uuid.NewString(), id, episode.id, generation, requiredAgent, requiredWireAPI, requiredProfileID, requiredProfileVersion, requiredProfileConcurrency, episode.payload, now.UTC()); err != nil {
				return nil, false, err
			}
			queued++
		}
	}
	if queued == 0 {
		return nil, true, status.Error(codes.FailedPrecondition, "rollout has no retryable infrastructure work")
	}
	if _, err := tx.Exec(ctx, `UPDATE rollouts SET status=$2, failure_class='FAILURE_CLASS_UNSPECIFIED', message='', completed_at=NULL, version=version+1 WHERE rollout_id=$1`, id, rolloutv1.RolloutStatus_ROLLOUT_STATUS_QUEUED.String()); err != nil {
		return nil, false, err
	}
	if err := insertEventTx(ctx, tx, id, "", "rollout.retried", "control", "retry queued", map[string]string{"work_items": strconv.FormatInt(queued, 10)}, now); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	updated, _, ok, err := s.Get(ctx, id)
	return updated, ok, err
}

func (s *Store) Delete(ctx context.Context, id string, now time.Time) (*rolloutv1.Rollout, bool, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanRollout(tx.QueryRow(ctx, rolloutSelectSQL()+` WHERE rollout_id=$1 FOR UPDATE`, id))
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	statusValue := record.rollout.GetStatus()
	if statusValue == rolloutv1.RolloutStatus_ROLLOUT_STATUS_READY {
		if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status='CANCELLED',updated_at=$2 WHERE rollout_id=$1 AND status='HELD'`, id, now.UTC()); err != nil {
			return nil, false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE rollout_episodes SET status='EPISODE_STATUS_CANCELLED',completed_at=$2 WHERE rollout_id=$1 AND status='EPISODE_STATUS_PENDING'`, id, now.UTC()); err != nil {
			return nil, false, err
		}
	} else if !rolloutkernel.IsTerminal(statusValue) && statusValue != rolloutv1.RolloutStatus_ROLLOUT_STATUS_DELETING {
		return nil, true, status.Error(codes.FailedPrecondition, "rollout must be READY or terminal before deletion")
	}
	if _, err := tx.Exec(ctx, `UPDATE rollouts SET status=$2,delete_requested_at=COALESCE(delete_requested_at,$3),version=version+1 WHERE rollout_id=$1 AND status<>$2`, id, rolloutv1.RolloutStatus_ROLLOUT_STATUS_DELETING.String(), now.UTC()); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	updated, _, ok, err := s.Get(ctx, id)
	return updated, ok, err
}

func (s *Store) ListEvents(ctx context.Context, rolloutID string, afterSequence int64, limit int) ([]*rolloutv1.RolloutEvent, int64, bool, error) {
	if limit <= 0 || limit > rolloutkernel.MaxPageSize {
		limit = rolloutkernel.MaxPageSize
	}
	rows, err := s.db.Pool().Query(ctx, `SELECT sequence,episode_id,event_type,phase,message,details,created_at FROM rollout_events WHERE rollout_id=$1 AND sequence>$2 ORDER BY sequence LIMIT $3`, rolloutID, afterSequence, limit)
	if err != nil {
		return nil, afterSequence, false, err
	}
	defer rows.Close()
	events := make([]*rolloutv1.RolloutEvent, 0)
	current := afterSequence
	for rows.Next() {
		var event rolloutv1.RolloutEvent
		var details []byte
		var createdAt time.Time
		if err := rows.Scan(&event.Sequence, &event.EpisodeID, &event.Type, &event.Phase, &event.Message, &details, &createdAt); err != nil {
			return nil, current, false, err
		}
		event.RolloutID = rolloutID
		_ = json.Unmarshal(details, &event.Details)
		event.CreatedAt = timestamppb.New(createdAt)
		events = append(events, &event)
		current = event.Sequence
	}
	var statusText string
	if err := s.db.Pool().QueryRow(ctx, `SELECT status FROM rollouts WHERE rollout_id=$1`, rolloutID).Scan(&statusText); err != nil {
		if err == pgx.ErrNoRows {
			return nil, current, false, status.Error(codes.NotFound, "rollout not found")
		}
		return nil, current, false, err
	}
	return events, current, rolloutkernel.IsTerminal(parseRolloutStatus(statusText)), rows.Err()
}

func (s *Store) Compare(ctx context.Context, rolloutIDs []string) ([]*rolloutv1.Rollout, []*rolloutv1.TaskComparison, error) {
	if len(rolloutIDs) < 2 || len(rolloutIDs) > 5 {
		return nil, nil, status.Error(codes.InvalidArgument, "compare requires two to five rollouts")
	}
	seen := map[string]struct{}{}
	type best struct {
		digest string
		reward float64
		passed bool
		cost   int64
		set    bool
	}
	byTask := map[string]map[string]best{}
	rollouts := make([]*rolloutv1.Rollout, 0, len(rolloutIDs))
	for _, id := range rolloutIDs {
		if _, duplicate := seen[id]; duplicate {
			return nil, nil, status.Error(codes.InvalidArgument, "rollout_ids must be unique")
		}
		seen[id] = struct{}{}
		rollout, episodes, ok, err := s.Get(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, status.Errorf(codes.NotFound, "rollout %s not found", id)
		}
		rollouts = append(rollouts, rollout)
		for _, episode := range episodes {
			if episode.GetStatus() != rolloutv1.EpisodeStatus_EPISODE_STATUS_COMPLETED {
				continue
			}
			if byTask[episode.GetTaskID()] == nil {
				byTask[episode.GetTaskID()] = map[string]best{}
			}
			current := byTask[episode.GetTaskID()][id]
			if !current.set || episode.GetReward() > current.reward {
				byTask[episode.GetTaskID()][id] = best{episode.GetTaskDigest(), episode.GetReward(), episode.GetPassed(), episode.GetCostMicrousd(), true}
			}
		}
	}
	ids := make([]string, 0, len(byTask))
	for id := range byTask {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	comparisons := make([]*rolloutv1.TaskComparison, 0, len(ids))
	for _, taskID := range ids {
		item := &rolloutv1.TaskComparison{TaskID: taskID, Rewards: map[string]float64{}, Passed: map[string]bool{}, CostMicrousd: map[string]int64{}, Comparable: true}
		digest := ""
		for _, rolloutID := range rolloutIDs {
			value, ok := byTask[taskID][rolloutID]
			if !ok {
				item.Comparable = false
				item.Reason = "task is missing from one or more rollouts"
				continue
			}
			item.Rewards[rolloutID] = value.reward
			item.Passed[rolloutID] = value.passed
			item.CostMicrousd[rolloutID] = value.cost
			if digest == "" {
				digest = value.digest
			} else if digest != value.digest {
				item.Comparable = false
				item.Reason = "task digest differs across rollouts"
			}
		}
		item.TaskDigest = digest
		comparisons = append(comparisons, item)
	}
	return rollouts, comparisons, nil
}
