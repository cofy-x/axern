package pgagentprofile

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	agentprofilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/agentprofile"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	pgsecret "github.com/cofy-x/axern/control/controld/internal/postgres/secret"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Store struct {
	db      *postgres.DB
	secrets *pgsecret.Store
}

func NewStore(db *postgres.DB, secrets *pgsecret.Store) *Store {
	return &Store{db: db, secrets: secrets}
}

func (s *Store) Create(ctx context.Context, params agentprofilekernel.CreateParams, now time.Time) (*agentprofilev1.AgentProfile, error) {
	if err := agentprofilekernel.ValidateCreate(params); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	namespace, name := strings.TrimSpace(params.Namespace), strings.TrimSpace(params.Name)
	requestHash := hashCreate(params)
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create profile: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, matched, err := operationResult(ctx, tx, namespace, name, "create", params.IdempotencyKey, requestHash); err != nil {
		return nil, err
	} else if matched {
		return existing, nil
	}
	if _, err := pgnamespace.Ensure(ctx, tx, namespace); err != nil {
		return nil, err
	}
	if err := lockProfileName(ctx, tx, namespace, name); err != nil {
		return nil, err
	}
	if existing, matched, err := operationResult(ctx, tx, namespace, name, "create", params.IdempotencyKey, requestHash); err != nil {
		return nil, err
	} else if matched {
		return existing, nil
	}
	profileID := "apf-" + uuid.NewString()
	credentialID, credentialVersion, err := s.secrets.CreateProfileCredentialTx(ctx, tx, namespace, profileID, params.Credential, now)
	if err != nil {
		return nil, err
	}
	profile := &agentprofilev1.AgentProfile{ID: profileID, Namespace: namespace, Name: name, Spec: proto.Clone(params.Spec).(*agentprofilev1.AgentProfileSpec), Labels: cloneMap(params.Labels), Version: 1, CredentialVersion: credentialVersion, CreatedAt: timestamppb.New(now.UTC()), UpdatedAt: timestamppb.New(now.UTC())}
	if err := insertProfile(ctx, tx, profile, credentialID, now); err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, status.Error(codes.AlreadyExists, "agent profile name already exists in namespace")
		}
		return nil, err
	}
	if err := recordOperation(ctx, tx, namespace, name, "create", params.IdempotencyKey, requestHash, profile, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create profile: %w", err)
	}
	return profile, nil
}

func (s *Store) Get(ctx context.Context, namespace, name string) (*agentprofilev1.AgentProfile, bool, error) {
	snapshot, ok, err := s.ResolveSnapshot(ctx, namespace, name)
	if !ok || err != nil {
		return nil, ok, err
	}
	return snapshot.Profile, true, nil
}

func (s *Store) ResolveSnapshot(ctx context.Context, namespace, name string) (*agentprofilekernel.Snapshot, bool, error) {
	return resolveSnapshotRow(s.db.Pool().QueryRow(ctx, profileSelectSQL()+` WHERE namespace = $1 AND name = $2`, strings.TrimSpace(namespace), strings.TrimSpace(name)))
}

func (s *Store) List(ctx context.Context, filter *agentprofilev1.AgentProfileListFilter) ([]*agentprofilev1.AgentProfile, string, error) {
	if filter == nil {
		filter = &agentprofilev1.AgentProfileListFilter{}
	}
	if strings.TrimSpace(filter.GetNamespace()) == "" {
		return nil, "", status.Error(codes.InvalidArgument, "filter.namespace is required")
	}
	pageSize := int(filter.GetPageSize())
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	labelsJSON, _ := json.Marshal(filter.GetLabels())
	provider := ""
	if filter.GetProvider() != agentprofilev1.AgentProvider_AGENT_PROVIDER_UNSPECIFIED {
		provider = filter.GetProvider().String()
	}
	var cursorTime time.Time
	var cursorID string
	if filter.GetCursor() != "" {
		var err error
		cursorTime, cursorID, err = decodeProfileCursor(filter.GetCursor())
		if err != nil {
			return nil, "", status.Error(codes.InvalidArgument, "invalid cursor")
		}
	}
	rows, err := s.db.Pool().Query(ctx, profileSelectSQL()+`
		WHERE namespace = $1 AND ($2 = '' OR spec->>'agent' = $2)
		  AND ($3 = '' OR spec->>'provider' = $3) AND labels @> $4::jsonb
		  AND ($5 = '' OR (created_at,profile_id) < ($6,$5))
		ORDER BY created_at DESC, profile_id DESC LIMIT $7
	`, strings.TrimSpace(filter.GetNamespace()), strings.TrimSpace(filter.GetAgent()), provider, labelsJSON, cursorID, cursorTime, pageSize+1)
	if err != nil {
		return nil, "", fmt.Errorf("list agent profiles: %w", err)
	}
	defer rows.Close()
	profiles := make([]*agentprofilev1.AgentProfile, 0, pageSize)
	for rows.Next() {
		snapshot, _, err := resolveSnapshotRow(rows)
		if err != nil {
			return nil, "", err
		}
		profiles = append(profiles, snapshot.Profile)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(profiles) > pageSize {
		next = encodeProfileCursor(profiles[pageSize-1].GetCreatedAt().AsTime(), profiles[pageSize-1].GetID())
		profiles = profiles[:pageSize]
	}
	return profiles, next, nil
}

func (s *Store) Update(ctx context.Context, params agentprofilekernel.UpdateParams, now time.Time) (*agentprofilev1.AgentProfile, error) {
	if params.ExpectedVersion <= 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_version is required")
	}
	if params.Patch == nil {
		return nil, status.Error(codes.InvalidArgument, "patch is required")
	}
	hash := hashProto(params.Patch, params.ExpectedVersion)
	return s.mutate(ctx, params.Namespace, params.Name, "update", params.IdempotencyKey, hash, now, func(ctx context.Context, tx pgx.Tx, snapshot *agentprofilekernel.Snapshot) error {
		if snapshot.Profile.GetVersion() != params.ExpectedVersion {
			return status.Error(codes.Aborted, "agent profile version conflict")
		}
		patch := params.Patch
		if patch.BaseUrl != nil {
			snapshot.Profile.Spec.BaseUrl = strings.TrimSpace(patch.GetBaseUrl())
		}
		if patch.MaxConcurrency != nil {
			snapshot.Profile.Spec.MaxConcurrency = patch.GetMaxConcurrency()
		}
		if patch.GetReplaceLabels() {
			snapshot.Profile.Labels = cloneMap(patch.GetLabels())
		}
		if err := validateStoredProfile(snapshot.Profile); err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		return updateProfileTx(ctx, tx, snapshot.Profile, snapshot.CredentialSecretID, now)
	})
}

func (s *Store) Rotate(ctx context.Context, params agentprofilekernel.RotateParams, now time.Time) (*agentprofilev1.AgentProfile, error) {
	if params.ExpectedVersion <= 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_version is required")
	}
	if err := agentprofilekernel.ValidateCredential(params.Credential); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	digest := sha256.Sum256(params.Credential)
	hash := hex.EncodeToString(digest[:]) + ":" + strconv.FormatInt(params.ExpectedVersion, 10)
	return s.mutate(ctx, params.Namespace, params.Name, "rotate", params.IdempotencyKey, hash, now, func(ctx context.Context, tx pgx.Tx, snapshot *agentprofilekernel.Snapshot) error {
		if snapshot.Profile.GetVersion() != params.ExpectedVersion {
			return status.Error(codes.Aborted, "agent profile version conflict")
		}
		credentialID, credentialVersion, err := s.secrets.CreateProfileCredentialTx(ctx, tx, snapshot.Profile.GetNamespace(), snapshot.Profile.GetID(), params.Credential, now)
		if err != nil {
			return err
		}
		snapshot.CredentialSecretID = credentialID
		snapshot.CredentialVersion++
		if snapshot.CredentialVersion < credentialVersion {
			snapshot.CredentialVersion = credentialVersion
		}
		snapshot.Profile.CredentialVersion = snapshot.CredentialVersion
		if err := updateProfileTx(ctx, tx, snapshot.Profile, credentialID, now); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM secrets s WHERE s.visibility='INTERNAL' AND s.owner_type='AGENT_PROFILE' AND s.owner_id=$1 AND s.secret_id<>$2
			AND NOT EXISTS(SELECT 1 FROM rollouts r WHERE r.frozen_credential_secret_id=s.secret_id)
			AND NOT EXISTS(SELECT 1 FROM agent_profile_doctor_jobs j WHERE j.frozen_credential_secret_id=s.secret_id)`, snapshot.Profile.GetID(), credentialID)
		return err
	})
}

func (s *Store) mutate(ctx context.Context, namespace, name, operation, key, requestHash string, now time.Time, apply func(context.Context, pgx.Tx, *agentprofilekernel.Snapshot) error) (*agentprofilev1.AgentProfile, error) {
	namespace, name = strings.TrimSpace(namespace), strings.TrimSpace(name)
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockProfileName(ctx, tx, namespace, name); err != nil {
		return nil, err
	}
	if existing, matched, err := operationResult(ctx, tx, namespace, name, operation, key, requestHash); err != nil {
		return nil, err
	} else if matched {
		return existing, nil
	}
	snapshot, ok, err := resolveSnapshotRow(tx.QueryRow(ctx, profileSelectSQL()+` WHERE namespace = $1 AND name = $2 FOR UPDATE`, namespace, name))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "agent profile not found")
	}
	if err := apply(ctx, tx, snapshot); err != nil {
		return nil, err
	}
	snapshot.Profile.Version++
	snapshot.Profile.UpdatedAt = timestamppb.New(now.UTC())
	if _, err := tx.Exec(ctx, `UPDATE agent_profiles SET version=$1, updated_at=$2 WHERE profile_id=$3`, snapshot.Profile.GetVersion(), now.UTC(), snapshot.Profile.GetID()); err != nil {
		return nil, err
	}
	if err := recordOperation(ctx, tx, namespace, name, operation, key, requestHash, snapshot.Profile, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return snapshot.Profile, nil
}

func (s *Store) Delete(ctx context.Context, namespace, name string, expectedVersion int64) (*agentprofilev1.AgentProfile, bool, error) {
	if expectedVersion <= 0 {
		return nil, false, status.Error(codes.InvalidArgument, "expected_version is required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	namespace, name = strings.TrimSpace(namespace), strings.TrimSpace(name)
	if err := lockProfileName(ctx, tx, namespace, name); err != nil {
		return nil, false, err
	}
	snapshot, ok, err := resolveSnapshotRow(tx.QueryRow(ctx, profileSelectSQL()+` WHERE namespace=$1 AND name=$2 FOR UPDATE`, namespace, name))
	if err != nil || !ok {
		return nil, ok, err
	}
	if snapshot.Profile.GetVersion() != expectedVersion {
		return nil, false, status.Error(codes.Aborted, "agent profile version conflict")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM agent_profiles WHERE profile_id=$1`, snapshot.Profile.GetID()); err != nil {
		if postgres.IsForeignKeyViolation(err) {
			return nil, false, status.Error(codes.FailedPrecondition, "agent profile is referenced by a rollout")
		}
		return nil, false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM secrets s
		WHERE s.visibility='INTERNAL' AND s.owner_type='AGENT_PROFILE' AND s.owner_id=$1
		  AND NOT EXISTS(SELECT 1 FROM rollouts r WHERE r.frozen_credential_secret_id=s.secret_id)
		  AND NOT EXISTS(SELECT 1 FROM agent_profile_doctor_jobs j WHERE j.frozen_credential_secret_id=s.secret_id)`, snapshot.Profile.GetID()); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return snapshot.Profile, true, nil
}

func (s *Store) Doctor(ctx context.Context, params agentprofilekernel.DoctorParams, now time.Time) (*agentprofilev1.DoctorAgentProfileResponse, error) {
	if strings.TrimSpace(params.Model) == "" {
		return nil, status.Error(codes.InvalidArgument, "model is required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	namespace, name := strings.TrimSpace(params.Namespace), strings.TrimSpace(params.Name)
	if err := lockProfileName(ctx, tx, namespace, name); err != nil {
		return nil, err
	}
	snapshot, ok, err := resolveSnapshotRow(tx.QueryRow(ctx, profileSelectSQL()+` WHERE namespace=$1 AND name=$2 FOR SHARE`, namespace, name))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "agent profile not found")
	}
	frozenJSON, err := protojson.Marshal(snapshot.Profile)
	if err != nil {
		return nil, err
	}
	jobID := "doctor-" + uuid.NewString()
	workID := "wrk-" + uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO agent_profile_doctor_jobs(job_id,profile_id,frozen_profile,frozen_credential_secret_id,model,status,created_at) VALUES($1,$2,$3::jsonb,$4,$5,'PENDING',$6)`, jobID, snapshot.Profile.GetID(), frozenJSON, snapshot.CredentialSecretID, strings.TrimSpace(params.Model), now.UTC()); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO rollout_work_items(work_id,kind,doctor_job_id,execution_generation,status,required_wire_api,required_profile_id,required_profile_version,required_profile_concurrency,payload,next_run_at,created_at,updated_at) VALUES($1,'WORK_KIND_PROFILE_DOCTOR',$2,1,'PENDING',$3,$4,$5,$6,'{}'::jsonb,$7,$7,$7)`, workID, jobID, snapshot.Profile.GetSpec().GetWireApi().String(), snapshot.Profile.GetID(), snapshot.Profile.GetVersion(), snapshot.Profile.GetSpec().GetMaxConcurrency(), now.UTC()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = cleanupDoctorJob(cleanupCtx, s.db, jobID)
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var statusText, errorText string
		var responseJSON []byte
		err := s.db.Pool().QueryRow(ctx, `SELECT status,checks,error FROM agent_profile_doctor_jobs WHERE job_id=$1`, jobID).Scan(&statusText, &responseJSON, &errorText)
		if err != nil {
			return nil, err
		}
		switch statusText {
		case "COMPLETED":
			response := &agentprofilev1.DoctorAgentProfileResponse{}
			if err := protojson.Unmarshal(responseJSON, response); err != nil {
				return nil, err
			}
			return response, nil
		case "FAILED":
			return nil, status.Error(codes.FailedPrecondition, errorText)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func cleanupDoctorJob(ctx context.Context, db *postgres.DB, jobID string) error {
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM agent_profile_doctor_jobs WHERE job_id=$1`, jobID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM secrets s
		WHERE s.visibility='INTERNAL' AND s.owner_type='AGENT_PROFILE'
		AND NOT EXISTS (SELECT 1 FROM agent_profiles p WHERE p.credential_secret_id=s.secret_id)
		AND NOT EXISTS (SELECT 1 FROM rollouts r WHERE r.frozen_credential_secret_id=s.secret_id)
		AND NOT EXISTS (SELECT 1 FROM agent_profile_doctor_jobs j WHERE j.frozen_credential_secret_id=s.secret_id)`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertProfile(ctx context.Context, tx pgx.Tx, profile *agentprofilev1.AgentProfile, credentialID string, now time.Time) error {
	specJSON, err := protojson.Marshal(profile.GetSpec())
	if err != nil {
		return err
	}
	labelsJSON, _ := json.Marshal(profile.GetLabels())
	_, err = tx.Exec(ctx, `INSERT INTO agent_profiles (profile_id,namespace,name,spec,credential_secret_id,credential_secret_version,labels,version,created_at,updated_at) VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7::jsonb,$8,$9,$9)`, profile.GetID(), profile.GetNamespace(), profile.GetName(), specJSON, credentialID, profile.GetCredentialVersion(), labelsJSON, profile.GetVersion(), now.UTC())
	return err
}

func updateProfileTx(ctx context.Context, tx pgx.Tx, profile *agentprofilev1.AgentProfile, credentialID string, now time.Time) error {
	specJSON, err := protojson.Marshal(profile.GetSpec())
	if err != nil {
		return err
	}
	labelsJSON, _ := json.Marshal(profile.GetLabels())
	_, err = tx.Exec(ctx, `UPDATE agent_profiles SET spec=$1::jsonb,labels=$2::jsonb,credential_secret_id=$3,credential_secret_version=$4,updated_at=$5 WHERE profile_id=$6`, specJSON, labelsJSON, credentialID, profile.GetCredentialVersion(), now.UTC(), profile.GetID())
	return err
}

func validateStoredProfile(profile *agentprofilev1.AgentProfile) error {
	return agentprofilekernel.ValidateCreate(agentprofilekernel.CreateParams{Namespace: profile.GetNamespace(), Name: profile.GetName(), Spec: profile.GetSpec(), Credential: []byte("validation-placeholder")})
}

func profileSelectSQL() string {
	return `SELECT profile_id,namespace,name,spec,labels,version,credential_secret_version,created_at,updated_at,credential_secret_id FROM agent_profiles`
}

func resolveSnapshotRow(row interface{ Scan(...any) error }) (*agentprofilekernel.Snapshot, bool, error) {
	var profile agentprofilev1.AgentProfile
	var specJSON, labelsJSON []byte
	var createdAt, updatedAt time.Time
	var credentialID string
	if err := row.Scan(&profile.ID, &profile.Namespace, &profile.Name, &specJSON, &labelsJSON, &profile.Version, &profile.CredentialVersion, &createdAt, &updatedAt, &credentialID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("scan agent profile: %w", err)
	}
	profile.Spec = &agentprofilev1.AgentProfileSpec{}
	if err := protojson.Unmarshal(specJSON, profile.Spec); err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(labelsJSON, &profile.Labels); err != nil {
		return nil, false, err
	}
	profile.CreatedAt = timestamppb.New(createdAt)
	profile.UpdatedAt = timestamppb.New(updatedAt)
	return &agentprofilekernel.Snapshot{Profile: &profile, CredentialSecretID: credentialID, CredentialVersion: profile.GetCredentialVersion()}, true, nil
}

func operationResult(ctx context.Context, tx pgx.Tx, namespace, name, operation, key, requestHash string) (*agentprofilev1.AgentProfile, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, nil
	}
	var storedName, storedHash string
	var resultJSON []byte
	err := tx.QueryRow(ctx, `SELECT profile_name,request_hash,result FROM agent_profile_operations WHERE namespace=$1 AND operation=$2 AND idempotency_key=$3`, namespace, operation, key).Scan(&storedName, &storedHash, &resultJSON)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if storedName != strings.TrimSpace(name) || storedHash != requestHash {
		return nil, false, status.Error(codes.AlreadyExists, "idempotency key already used with different parameters")
	}
	profile := &agentprofilev1.AgentProfile{}
	if err := protojson.Unmarshal(resultJSON, profile); err != nil {
		return nil, false, err
	}
	return profile, true, nil
}
func recordOperation(ctx context.Context, tx pgx.Tx, namespace, name, operation, key, requestHash string, profile *agentprofilev1.AgentProfile, now time.Time) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	resultJSON, err := protojson.Marshal(profile)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_profile_operations(namespace,profile_name,operation,idempotency_key,request_hash,profile_id,result,created_at) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8)`, namespace, name, operation, strings.TrimSpace(key), requestHash, profile.GetID(), resultJSON, now.UTC())
	return err
}
func hashCreate(params agentprofilekernel.CreateParams) string {
	spec, _ := proto.MarshalOptions{Deterministic: true}.Marshal(params.Spec)
	cred := sha256.Sum256(params.Credential)
	labels, _ := json.Marshal(params.Labels)
	sum := sha256.Sum256(append(append(append([]byte(params.Namespace+"\x00"+params.Name+"\x00"), spec...), cred[:]...), labels...))
	return hex.EncodeToString(sum[:])
}
func hashProto(value proto.Message, version int64) string {
	data, _ := proto.MarshalOptions{Deterministic: true}.Marshal(value)
	sum := sha256.Sum256(append(data, []byte(strconv.FormatInt(version, 10))...))
	return hex.EncodeToString(sum[:])
}
func encodeProfileCursor(createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(createdAt.UnixNano(), 10) + "\x00" + id))
}
func decodeProfileCursor(value string) (time.Time, string, error) {
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
func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func lockProfileName(ctx context.Context, tx pgx.Tx, namespace, name string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(length($1)::text || ':' || $1 || $2, 0))`, strings.TrimSpace(namespace), strings.TrimSpace(name))
	return err
}

var _ agentprofilekernel.Control = (*Store)(nil)
