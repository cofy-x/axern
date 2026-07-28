package pgrollout

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) CreateArtifactUpload(ctx context.Context, req *workerrolloutv1.CreateArtifactUploadRequest, leaseTokenHash string, now time.Time, ttl time.Duration) (*rolloutv1.Artifact, rolloutkernel.ArtifactUpload, error) {
	if s.artifacts == nil {
		return nil, rolloutkernel.ArtifactUpload{}, status.Error(codes.FailedPrecondition, "rollout artifact store is not configured")
	}
	name := strings.TrimSpace(req.GetName())
	if req == nil || name == "" || name != path.Base(name) || strings.ContainsAny(name, "/\\") || strings.TrimSpace(req.GetKind()) == "" || strings.TrimSpace(req.GetMediaType()) == "" || req.GetSizeBytes() < 0 {
		return nil, rolloutkernel.ArtifactUpload{}, status.Error(codes.InvalidArgument, "valid kind, base name, media_type, and non-negative size_bytes are required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, rolloutkernel.ArtifactUpload{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	work, err := lockActiveWork(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		return nil, rolloutkernel.ArtifactUpload{}, err
	}
	existing, existingErr := scanArtifact(tx.QueryRow(ctx, artifactSelectSQL()+` WHERE rollout_id=$1 AND episode_id=$2 AND execution_generation=$3 AND kind=$4 AND name=$5 FOR UPDATE`, work.rolloutID, work.episodeID, work.generation, req.GetKind(), name))
	if existingErr != nil && existingErr != pgx.ErrNoRows {
		return nil, rolloutkernel.ArtifactUpload{}, existingErr
	}
	if existingErr == nil {
		artifact := existing.artifact
		if artifact.GetDigest() != req.GetDigest() || artifact.GetSizeBytes() != req.GetSizeBytes() || artifact.GetMediaType() != req.GetMediaType() {
			return nil, rolloutkernel.ArtifactUpload{}, status.Error(codes.AlreadyExists, "artifact name already exists with different content")
		}
		upload, err := s.artifacts.PresignUpload(ctx, existing.objectKey, req.GetMediaType(), req.GetSizeBytes(), req.GetDigest(), ttl)
		if err != nil {
			return nil, rolloutkernel.ArtifactUpload{}, fmt.Errorf("presign existing artifact upload: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, rolloutkernel.ArtifactUpload{}, err
		}
		return artifact, upload, nil
	}
	artifactID := "art-" + uuid.NewString()
	objectKey := fmt.Sprintf("rollouts/%s/episodes/%s/g%d/%s/%s", work.rolloutID, defaultString(work.episodeID, "control"), work.generation, artifactID, name)
	upload, err := s.artifacts.PresignUpload(ctx, objectKey, req.GetMediaType(), req.GetSizeBytes(), req.GetDigest(), ttl)
	if err != nil {
		return nil, rolloutkernel.ArtifactUpload{}, fmt.Errorf("presign artifact upload: %w", err)
	}
	artifact := &rolloutv1.Artifact{ID: artifactID, RolloutID: work.rolloutID, EpisodeID: work.episodeID, ExecutionGeneration: work.generation, Kind: req.GetKind(), Name: name, MediaType: req.GetMediaType(), SizeBytes: req.GetSizeBytes(), Digest: req.GetDigest(), Status: rolloutv1.ArtifactStatus_ARTIFACT_STATUS_PENDING, CreatedAt: timestamppb.New(now.UTC())}
	if _, err := tx.Exec(ctx, `INSERT INTO rollout_artifacts(artifact_id,rollout_id,episode_id,execution_generation,kind,name,object_key,media_type,size_bytes,digest,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, artifact.GetID(), artifact.GetRolloutID(), artifact.GetEpisodeID(), artifact.GetExecutionGeneration(), artifact.GetKind(), artifact.GetName(), objectKey, artifact.GetMediaType(), artifact.GetSizeBytes(), artifact.GetDigest(), artifact.GetStatus().String(), now.UTC()); err != nil {
		return nil, rolloutkernel.ArtifactUpload{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, rolloutkernel.ArtifactUpload{}, err
	}
	return artifact, upload, nil
}

func (s *Store) CommitArtifact(ctx context.Context, req *workerrolloutv1.CommitArtifactRequest, leaseTokenHash string, now time.Time) (*rolloutv1.Artifact, error) {
	if s.artifacts == nil {
		return nil, status.Error(codes.FailedPrecondition, "rollout artifact store is not configured")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	work, err := lockActiveWork(ctx, tx, req.GetWorkID(), leaseTokenHash, now)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	record, err := scanArtifact(tx.QueryRow(ctx, artifactSelectSQL()+` WHERE artifact_id=$1 AND rollout_id=$2 AND episode_id=$3 AND execution_generation=$4`, req.GetArtifactID(), work.rolloutID, work.episodeID, work.generation))
	_ = tx.Rollback(ctx)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, "artifact not found for work generation")
	}
	if err != nil {
		return nil, err
	}
	artifact, objectKey := record.artifact, record.objectKey
	if artifact.GetStatus() == rolloutv1.ArtifactStatus_ARTIFACT_STATUS_PRESENT {
		return artifact, nil
	}
	if err := s.artifacts.Verify(ctx, objectKey, artifact.GetSizeBytes(), artifact.GetDigest()); err != nil {
		_, _ = s.db.Pool().Exec(ctx, `UPDATE rollout_artifacts SET status='ARTIFACT_STATUS_FAILED',message=$2 WHERE artifact_id=$1`, artifact.GetID(), err.Error())
		return nil, status.Error(codes.FailedPrecondition, "uploaded artifact failed integrity verification")
	}
	record, err = scanArtifact(s.db.Pool().QueryRow(ctx, `UPDATE rollout_artifacts a SET status='ARTIFACT_STATUS_PRESENT',committed_at=$4 FROM rollout_work_items w WHERE a.artifact_id=$1 AND w.work_id=$2 AND w.status='LEASED' AND w.lease_token_hash=$3 AND w.lease_expires_at>$4 RETURNING a.artifact_id,a.rollout_id,a.episode_id,a.execution_generation,a.kind,a.name,a.object_key,a.media_type,a.size_bytes,a.digest,a.status,a.message,a.created_at`, artifact.GetID(), req.GetWorkID(), leaseTokenHash, now.UTC()))
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.FailedPrecondition, "work lease expired before artifact commit")
	}
	if err != nil {
		return nil, err
	}
	return record.artifact, nil
}

// ReconcileDeletes removes object-store evidence before deleting the durable
// database aggregate. A failed object-store deletion leaves the rollout in
// DELETING so a later reconciliation can safely retry it.
func (s *Store) ReconcileDeletes(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 32
	}
	rows, err := s.db.Pool().Query(ctx, `SELECT rollout_id FROM rollouts WHERE status='ROLLOUT_STATUS_DELETING' ORDER BY delete_requested_at,rollout_id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, id := range ids {
		if s.artifacts != nil {
			if err := s.artifacts.DeletePrefix(ctx, "rollouts/"+id+"/"); err != nil {
				return deleted, fmt.Errorf("delete artifacts for %s: %w", id, err)
			}
		}
		tx, err := s.db.Pool().Begin(ctx)
		if err != nil {
			return deleted, err
		}
		command, err := tx.Exec(ctx, `DELETE FROM rollouts WHERE rollout_id=$1 AND status='ROLLOUT_STATUS_DELETING'`, id)
		if err != nil {
			_ = tx.Rollback(ctx)
			return deleted, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM secrets s
			WHERE s.visibility='INTERNAL' AND s.owner_type='AGENT_PROFILE'
			AND NOT EXISTS (SELECT 1 FROM agent_profiles p WHERE p.credential_secret_id=s.secret_id)
			AND NOT EXISTS (SELECT 1 FROM rollouts r WHERE r.frozen_credential_secret_id=s.secret_id)
			AND NOT EXISTS (SELECT 1 FROM agent_profile_doctor_jobs j WHERE j.frozen_credential_secret_id=s.secret_id)`); err != nil {
			_ = tx.Rollback(ctx)
			return deleted, err
		}
		if err := tx.Commit(ctx); err != nil {
			return deleted, err
		}
		deleted += int(command.RowsAffected())
	}
	return deleted, nil
}

type artifactRecord struct {
	artifact  *rolloutv1.Artifact
	objectKey string
}

func artifactSelectSQL() string {
	return `SELECT artifact_id,rollout_id,episode_id,execution_generation,kind,name,object_key,media_type,size_bytes,digest,status,message,created_at FROM rollout_artifacts`
}
func scanArtifact(row interface{ Scan(...any) error }) (*artifactRecord, error) {
	artifact := &rolloutv1.Artifact{}
	var statusText, key string
	var createdAt time.Time
	if err := row.Scan(&artifact.ID, &artifact.RolloutID, &artifact.EpisodeID, &artifact.ExecutionGeneration, &artifact.Kind, &artifact.Name, &key, &artifact.MediaType, &artifact.SizeBytes, &artifact.Digest, &statusText, &artifact.Message, &createdAt); err != nil {
		return nil, err
	}
	if value, ok := rolloutv1.ArtifactStatus_value[statusText]; ok {
		artifact.Status = rolloutv1.ArtifactStatus(value)
	}
	artifact.CreatedAt = timestamppb.New(createdAt)
	return &artifactRecord{artifact: artifact, objectKey: key}, nil
}
func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
