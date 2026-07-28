package postgres

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

const reclaimLeaseDuration = 60 * time.Second

type reclaimCandidate struct {
	claim      *storagev1.VolumeClaim
	backend    storagev1.VolumeBackend
	generation int64
}

func (s *Store) ClaimVolumeReclaims(ctx context.Context, req *privatestoragev1.ClaimVolumeReclaimsRequest) ([]*privatestoragev1.VolumeReclaim, error) {
	owner := strings.TrimSpace(req.GetLeaseOwner())
	if owner == "" {
		return nil, fmt.Errorf("volume reclaim lease owner is required")
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT c.payload, vc.backend, c.reclaim_generation
		FROM storage_volume_claims c
		JOIN storage_volume_classes vc ON vc.name = c.class_name
		WHERE c.status = $1
		  AND c.payload->>'reclaim_policy' = $2
		  AND NULLIF(btrim(c.payload->'topology'->>'node_id'), '') IS NOT NULL
		  AND (c.next_reclaim_at IS NULL OR c.next_reclaim_at <= $3)
		  AND (c.reclaim_lease_until IS NULL OR c.reclaim_lease_until <= $3)
		  AND (COALESCE(cardinality($4::text[]), 0) = 0 OR NOT ((c.payload->'topology'->>'node_id') = ANY($4::text[])))
		  AND NOT EXISTS (
			SELECT 1 FROM storage_volume_bindings b
			WHERE b.claim_id = c.claim_id AND b.status <> $5
		  )
		ORDER BY COALESCE(c.next_reclaim_at, c.updated_at), c.updated_at, c.claim_id
		FOR UPDATE OF c SKIP LOCKED
		LIMIT $6
	`, storagev1.VolumeStatus_VOLUME_STATUS_DELETING.String(), storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE.String(),
		now, req.GetExcludedNodeIds(), storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String(), limit)
	if err != nil {
		return nil, err
	}
	var candidates []reclaimCandidate
	for rows.Next() {
		var payload []byte
		var backendName string
		var generation int64
		if err := rows.Scan(&payload, &backendName, &generation); err != nil {
			rows.Close()
			return nil, err
		}
		claim := &storagev1.VolumeClaim{}
		if err := protojson.Unmarshal(payload, claim); err != nil {
			rows.Close()
			return nil, err
		}
		backend, ok := storagev1.VolumeBackend_value[backendName]
		if !ok {
			rows.Close()
			return nil, fmt.Errorf("volume class backend %q is invalid", backendName)
		}
		candidates = append(candidates, reclaimCandidate{claim: claim, backend: storagev1.VolumeBackend(backend), generation: generation + 1})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	leaseUntil := now.Add(reclaimLeaseDuration)
	out := make([]*privatestoragev1.VolumeReclaim, 0, len(candidates))
	for _, candidate := range candidates {
		token := uuid.NewString()
		tokenHash := sha256.Sum256([]byte(token))
		candidate.claim.ReclaimLeaseToken = ""
		candidate.claim.ReclaimLeaseUntil = timestamp(leaseUntil)
		candidate.claim.Version++
		candidate.claim.UpdatedAt = timestamp(now)
		if err := s.updateVolumeClaim(ctx, tx, candidate.claim); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE storage_volume_claims
			SET reclaim_lease_owner=$2, reclaim_lease_token_hash=$3, reclaim_lease_until=$4, reclaim_generation=$5
			WHERE claim_id=$1
		`, candidate.claim.GetID(), owner, tokenHash[:], leaseUntil, candidate.generation); err != nil {
			return nil, err
		}
		out = append(out, &privatestoragev1.VolumeReclaim{
			ClaimID: candidate.claim.GetID(), Namespace: candidate.claim.GetNamespace(), ClaimName: candidate.claim.GetName(),
			WorkloadID: candidate.claim.GetOwnerID(), NodeID: candidate.claim.GetTopology().GetNodeID(), Backend: candidate.backend,
			BackendHandle: candidate.claim.GetBackendHandle(), Attempt: candidate.claim.GetReclaimAttempt() + 1,
			LastError: candidate.claim.GetMessage(), NextRetryAt: candidate.claim.GetNextReclaimAt(), UpdatedAt: candidate.claim.GetUpdatedAt(),
			LeaseToken: token, LeaseOwner: owner, LeaseGeneration: candidate.generation,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ReportVolumeReclaim(ctx context.Context, req *privatestoragev1.ReportVolumeReclaimRequest) error {
	claimID := strings.TrimSpace(req.GetClaimID())
	owner := strings.TrimSpace(req.GetLeaseOwner())
	if claimID == "" || owner == "" || strings.TrimSpace(req.GetLeaseToken()) == "" || req.GetLeaseGeneration() <= 0 {
		return fmt.Errorf("volume reclaim claim, lease owner, token, and generation are required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var payload, storedHash []byte
	var storedOwner string
	var storedGeneration int64
	var leaseUntil, now time.Time
	err = tx.QueryRow(ctx, `
		SELECT payload, reclaim_lease_owner, reclaim_lease_token_hash, reclaim_generation, reclaim_lease_until, clock_timestamp()
		FROM storage_volume_claims WHERE claim_id=$1 FOR UPDATE
	`, claimID).Scan(&payload, &storedOwner, &storedHash, &storedGeneration, &leaseUntil, &now)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	claim := &storagev1.VolumeClaim{}
	if err := protojson.Unmarshal(payload, claim); err != nil {
		return err
	}
	if claim.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		return nil
	}
	presentedHash := sha256.Sum256([]byte(req.GetLeaseToken()))
	if claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETING || claim.GetTopology().GetNodeID() != strings.TrimSpace(req.GetNodeID()) ||
		storedOwner != owner || storedGeneration != req.GetLeaseGeneration() || len(storedHash) != sha256.Size ||
		subtle.ConstantTimeCompare(storedHash, presentedHash[:]) != 1 || !leaseUntil.After(now) {
		return fmt.Errorf("volume claim %q reclaim lease is stale or not owned by this worker", claimID)
	}
	claim.ReclaimLeaseToken = ""
	claim.ReclaimLeaseUntil = nil
	claim.Version++
	claim.UpdatedAt = timestamp(now)
	if req.GetSucceeded() {
		claim.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETED
		claim.Message = "volume backend reclaimed"
		claim.NextReclaimAt = nil
	} else {
		claim.ReclaimAttempt++
		claim.Message = strings.TrimSpace(req.GetMessage())
		delay := 2 * time.Second * time.Duration(1<<min(claim.GetReclaimAttempt()-1, 4))
		claim.NextReclaimAt = timestamp(now.Add(delay))
	}
	if err := s.updateVolumeClaim(ctx, tx, claim); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storage_volume_claims
		SET reclaim_lease_owner=NULL, reclaim_lease_token_hash=NULL, reclaim_lease_until=NULL,
			next_reclaim_at=$2, reclaim_attempt=$3
		WHERE claim_id=$1
	`, claimID, timestampOrNil(claim.GetNextReclaimAt()), claim.GetReclaimAttempt()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetVolumeReclaimQueueHealth(ctx context.Context) (*privatestoragev1.VolumeReclaimQueueHealth, error) {
	health := &privatestoragev1.VolumeReclaimQueueHealth{}
	err := s.db.Pool().QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE (next_reclaim_at IS NULL OR next_reclaim_at<=clock_timestamp()) AND reclaim_lease_until IS NULL),
			count(*) FILTER (WHERE next_reclaim_at>clock_timestamp() AND reclaim_lease_until IS NULL),
			count(*) FILTER (WHERE reclaim_lease_until>clock_timestamp()),
			count(*) FILTER (WHERE reclaim_lease_until<=clock_timestamp()),
			COALESCE(EXTRACT(EPOCH FROM clock_timestamp()-min(COALESCE(next_reclaim_at,updated_at)) FILTER (
				WHERE (next_reclaim_at IS NULL OR next_reclaim_at<=clock_timestamp()) AND reclaim_lease_until IS NULL
			)),0)
		FROM storage_volume_claims c
		WHERE c.status=$1
		  AND c.payload->>'reclaim_policy'=$2
		  AND NULLIF(btrim(c.payload->'topology'->>'node_id'), '') IS NOT NULL
		  AND NOT EXISTS (
			SELECT 1 FROM storage_volume_bindings b
			WHERE b.claim_id=c.claim_id AND b.status<>$3
		  )
	`, storagev1.VolumeStatus_VOLUME_STATUS_DELETING.String(), storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE.String(),
		storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String()).Scan(
		&health.Due, &health.Scheduled, &health.LeasedActive, &health.LeasedExpired, &health.OldestDueAgeSeconds,
	)
	if err != nil {
		return nil, err
	}
	return health, nil
}
