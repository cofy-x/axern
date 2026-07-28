package pgrollout

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentprofilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/agentprofile"
	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	workStatusPending   = "PENDING"
	workStatusLeased    = "LEASED"
	workStatusCompleted = "COMPLETED"
)

type Store struct {
	db                *postgres.DB
	profiles          agentprofilekernel.Control
	secrets           secretkernel.ProfileCredentialResolver
	artifacts         rolloutkernel.ArtifactStore
	artifactTicketKey []byte
	notifications     *notificationHub
	now               func() time.Time
}

type Option func(*Store)

func WithArtifactStore(store rolloutkernel.ArtifactStore) Option {
	return func(value *Store) { value.artifacts = store }
}
func WithArtifactTicketKey(key []byte) Option {
	return func(value *Store) { value.artifactTicketKey = append([]byte(nil), key...) }
}
func WithNow(now func() time.Time) Option {
	return func(value *Store) { value.now = now }
}
func NewStore(db *postgres.DB, profiles agentprofilekernel.Control, secrets secretkernel.ProfileCredentialResolver, options ...Option) *Store {
	store := &Store{
		db:            db,
		profiles:      profiles,
		secrets:       secrets,
		notifications: newNotificationHub(db.Pool()),
		now:           time.Now,
	}
	for _, option := range options {
		option(store)
	}
	if store.now == nil {
		store.now = time.Now
	}
	return store
}

func (s *Store) StartNotifications() {
	if s != nil && s.notifications != nil {
		s.notifications.start()
	}
}

func (s *Store) Close() {
	if s != nil && s.notifications != nil {
		s.notifications.close()
	}
}

func (s *Store) NotificationStats() NotificationStats {
	if s == nil || s.notifications == nil {
		return NotificationStats{}
	}
	return s.notifications.stats()
}

func (s *Store) Create(ctx context.Context, params rolloutkernel.CreateParams, now time.Time) (*rolloutv1.Rollout, error) {
	if err := rolloutkernel.ValidateCreate(params); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	specHash, err := rolloutkernel.SpecHash(params.Namespace, params.Spec, params.Labels, params.StartPolicy)
	if err != nil {
		return nil, err
	}
	rolloutID := "rol-" + uuid.NewString()
	rollout := &rolloutv1.Rollout{
		ID:          rolloutID,
		Namespace:   strings.TrimSpace(params.Namespace),
		Spec:        params.Spec,
		Status:      rolloutv1.RolloutStatus_ROLLOUT_STATUS_ACCEPTED,
		Summary:     &rolloutv1.RolloutSummary{},
		Labels:      cloneMap(params.Labels),
		Version:     1,
		CreatedAt:   timestamppb.New(now.UTC()),
		StartPolicy: params.StartPolicy,
	}
	if budget := params.Spec.GetBudget(); budget != nil && budget.GetMaxWallTime() != nil {
		deadline := now.UTC().Add(budget.GetMaxWallTime().AsDuration())
		rollout.Deadline = timestamppb.New(deadline)
	}
	specJSON, err := protojson.Marshal(rollout.GetSpec())
	if err != nil {
		return nil, fmt.Errorf("marshal rollout spec: %w", err)
	}
	labelsJSON, _ := json.Marshal(rollout.GetLabels())
	summaryJSON, _ := protojson.Marshal(rollout.GetSummary())
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create rollout: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := pgnamespace.Ensure(ctx, tx, strings.TrimSpace(params.Namespace)); err != nil {
		return nil, err
	}
	var profileID, credentialID, requiredWireAPI string
	var credentialVersion, profileVersion int64
	var profileConcurrency int32
	var frozenProfileJSON []byte
	if profileName := strings.TrimSpace(params.Spec.GetAgent().GetProfile()); profileName != "" {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(length($1)::text || ':' || $1 || $2, 0))`, strings.TrimSpace(params.Namespace), profileName); err != nil {
			return nil, err
		}
		snapshot, err := s.validateProfile(ctx, params.Namespace, params.Spec)
		if err != nil {
			return nil, err
		}
		profileID = snapshot.Profile.GetID()
		credentialID = snapshot.CredentialSecretID
		credentialVersion = snapshot.CredentialVersion
		profileVersion = snapshot.Profile.GetVersion()
		profileConcurrency = snapshot.Profile.GetSpec().GetMaxConcurrency()
		requiredWireAPI = snapshot.Profile.GetSpec().GetWireApi().String()
		frozenProfileJSON, err = protojson.Marshal(snapshot.Profile)
		if err != nil {
			return nil, fmt.Errorf("marshal frozen profile: %w", err)
		}
	}
	commandTag, err := tx.Exec(ctx, `
		INSERT INTO rollouts (
			rollout_id, namespace, status, spec, spec_hash, idempotency_key, profile_id,
			summary, labels, version, created_at, deadline, start_policy, frozen_profile,
			frozen_credential_secret_id, frozen_credential_version
		) VALUES ($1, $2, $3, $4::jsonb, $5, NULLIF($6, ''), NULLIF($7, ''),
			$8::jsonb, $9::jsonb, $10, $11, $12, $13, NULLIF($14::text,'')::jsonb,
			NULLIF($15,''), NULLIF($16,0))
		ON CONFLICT (namespace, idempotency_key) DO NOTHING
	`, rollout.GetID(), rollout.GetNamespace(), rollout.GetStatus().String(), specJSON, specHash,
		strings.TrimSpace(params.IdempotencyKey), profileID,
		summaryJSON, labelsJSON, rollout.GetVersion(), now.UTC(), timestampOrNil(rollout.GetDeadline()), params.StartPolicy.String(), string(frozenProfileJSON), credentialID, credentialVersion)
	if err != nil {
		return nil, fmt.Errorf("insert rollout: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		existing, lookupErr := getRolloutByIdempotencyTx(ctx, tx, params.Namespace, params.IdempotencyKey)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if existing.specHash != specHash {
			return nil, status.Error(codes.AlreadyExists, "idempotency key already exists with different rollout parameters")
		}
		return existing.rollout, nil
	}
	workID := "wrk-" + uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO rollout_work_items (
			work_id, kind, rollout_id, execution_generation, status, required_wire_api,
			required_profile_id, required_profile_version, required_profile_concurrency, payload,
			next_run_at, created_at, updated_at
		) VALUES ($1, 'WORK_KIND_PLAN', $2, 1, $3, $4, $5, $6, $7, '{}'::jsonb, $8, $8, $8)
	`, workID, rollout.GetID(), workStatusPending, requiredWireAPI, profileID, profileVersion, profileConcurrency, now.UTC()); err != nil {
		return nil, fmt.Errorf("insert planning work: %w", err)
	}
	if err := insertEventTx(ctx, tx, rollout.GetID(), "", "rollout.accepted", "planning", "rollout accepted", nil, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create rollout: %w", err)
	}
	return rollout, nil
}

func (s *Store) validateProfile(ctx context.Context, namespace string, spec *rolloutv1.RolloutSpec) (*agentprofilekernel.Snapshot, error) {
	profileName := strings.TrimSpace(spec.GetAgent().GetProfile())
	if profileName == "" {
		return nil, nil
	}
	snapshot, ok, err := s.profiles.ResolveSnapshot(ctx, strings.TrimSpace(namespace), profileName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "agent profile was not found in rollout namespace")
	}
	if snapshot.Profile.GetSpec().GetAgent() != spec.GetAgent().GetName() {
		return nil, status.Error(codes.FailedPrecondition, "agent profile does not match rollout agent")
	}
	return snapshot, nil
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func timestampOrNil(value *timestamppb.Timestamp) any {
	if value == nil {
		return nil
	}
	return value.AsTime().UTC()
}

func insertEventTx(ctx context.Context, tx pgx.Tx, rolloutID, episodeID, eventType, phase, message string, details map[string]string, now time.Time) error {
	detailsJSON, _ := json.Marshal(details)
	if _, err := tx.Exec(ctx, `
		INSERT INTO rollout_events (rollout_id, episode_id, event_type, phase, message, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
	`, rolloutID, episodeID, eventType, phase, message, detailsJSON, now.UTC()); err != nil {
		return fmt.Errorf("insert rollout event: %w", err)
	}
	return nil
}

var (
	_ rolloutkernel.Store       = (*Store)(nil)
	_ rolloutkernel.WorkerStore = (*Store)(nil)
	_                           = workerrolloutv1.WorkKind_WORK_KIND_PLAN
)
