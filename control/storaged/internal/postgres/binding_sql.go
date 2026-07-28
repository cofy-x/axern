package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) lockVolumeBinding(ctx context.Context, tx pgx.Tx, allocationID, nodeID, bindingID string, requireActive bool) (*privatestoragev1.VolumeBinding, error) {
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return nil, fmt.Errorf("volume binding id is required")
	}
	query := `
		SELECT payload FROM storage_volume_bindings
		WHERE allocation_id = $1
		  AND binding_id = $2
		  AND ($3 = '' OR node_id = $3)
	`
	args := []any{allocationID, bindingID, nodeID}
	if requireActive {
		query += ` AND status <> $4`
		args = append(args, storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String())
	}
	query += `
		FOR UPDATE
	`
	row := tx.QueryRow(ctx, query, args...)
	binding, err := scanVolumeBinding(row)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("volume binding %q not found", bindingID)
	}
	if err != nil {
		return nil, err
	}
	return binding, nil
}

func (s *Store) lockVolumeBindingByID(ctx context.Context, tx pgx.Tx, bindingID string) (*privatestoragev1.VolumeBinding, error) {
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return nil, fmt.Errorf("volume binding id is required")
	}
	binding, err := scanVolumeBinding(tx.QueryRow(ctx, `
		SELECT payload FROM storage_volume_bindings
		WHERE binding_id = $1
		  AND status <> $2
		FOR UPDATE
	`, bindingID, storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String()))
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("volume binding %q not found", bindingID)
	}
	if err != nil {
		return nil, err
	}
	return binding, nil
}

func (s *Store) releaseTargetBindings(ctx context.Context, tx pgx.Tx, allocationID, nodeID string, observations []*privatestoragev1.VolumeReleaseObservation) ([]*privatestoragev1.VolumeBinding, error) {
	if len(observations) > 0 {
		out := make([]*privatestoragev1.VolumeBinding, 0, len(observations))
		for _, observation := range observations {
			if observation == nil {
				continue
			}
			if err := kernel.ValidateVolumeReleaseObservation(observation); err != nil {
				return nil, err
			}
			binding, err := s.lockVolumeBinding(ctx, tx, allocationID, nodeID, observation.GetBindingID(), false)
			if err != nil {
				return nil, err
			}
			out = append(out, binding)
		}
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT payload FROM storage_volume_bindings
		WHERE allocation_id = $1
		  AND ($2 = '' OR node_id = $2)
		  AND status <> $3
		FOR UPDATE
	`, allocationID, nodeID, storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*privatestoragev1.VolumeBinding
	for rows.Next() {
		binding, err := scanVolumeBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func (s *Store) updateVolumeBinding(ctx context.Context, tx pgx.Tx, binding *privatestoragev1.VolumeBinding) error {
	payload, err := marshalProto(binding)
	if err != nil {
		return err
	}
	var publishedAt any
	if binding.GetPublishedAt() != nil {
		publishedAt = binding.GetPublishedAt().AsTime()
	}
	var releasedAt any
	if binding.GetReleasedAt() != nil {
		releasedAt = binding.GetReleasedAt().AsTime()
	}
	_, err = tx.Exec(ctx, `
		UPDATE storage_volume_bindings
		SET status = $2,
		    payload = $3::jsonb,
		    message = $4,
		    published_at = $5,
		    released_at = $6,
		    updated_at = $7
		WHERE binding_id = $1
	`, binding.GetBindingID(), binding.GetStatus().String(), payload, binding.GetMessage(), publishedAt, releasedAt, binding.GetUpdatedAt().AsTime())
	return err
}

func (s *Store) updateClaimFromBindingStatus(ctx context.Context, tx pgx.Tx, claimID string, status storagev1.VolumeStatus, topology *storagev1.VolumeTopology, message string, now time.Time) error {
	claim, err := scanVolumeClaim(tx.QueryRow(ctx, `
		SELECT payload FROM storage_volume_claims
		WHERE claim_id = $1
		FOR UPDATE
	`, claimID))
	if err != nil {
		return err
	}
	nextStatus := status
	if status == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		nextStatus = storagev1.VolumeStatus_VOLUME_STATUS_BOUND
	}
	if err := kernel.TransitionClaimStatus(claim.GetStatus(), nextStatus); err != nil {
		return err
	}
	claim.Status = nextStatus
	claim.Topology = cloneTopology(topology)
	claim.Message = strings.TrimSpace(message)
	claim.Version++
	claim.UpdatedAt = timestamppb.New(now)
	return s.updateVolumeClaim(ctx, tx, claim)
}

func (s *Store) recomputeClaimStatus(ctx context.Context, tx pgx.Tx, claimID string, now time.Time) error {
	claim, err := scanVolumeClaim(tx.QueryRow(ctx, `
		SELECT payload FROM storage_volume_claims
		WHERE claim_id = $1
		FOR UPDATE
	`, claimID))
	if err != nil {
		return err
	}
	if claim.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT payload FROM storage_volume_bindings
		WHERE claim_id = $1
		  AND status <> $2
	`, claimID, storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String())
	if err != nil {
		return err
	}
	defer rows.Close()
	nextStatus := storagev1.VolumeStatus_VOLUME_STATUS_BOUND
	message := ""
	for rows.Next() {
		binding, err := scanVolumeBinding(rows)
		if err != nil {
			return err
		}
		switch binding.GetStatus() {
		case storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
			nextStatus = storagev1.VolumeStatus_VOLUME_STATUS_FAILED
			message = binding.GetMessage()
		case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:
			if nextStatus != storagev1.VolumeStatus_VOLUME_STATUS_FAILED {
				nextStatus = storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if claim.GetStatus() == nextStatus && claim.GetMessage() == message {
		return nil
	}
	if err := kernel.TransitionClaimStatus(claim.GetStatus(), nextStatus); err != nil {
		return err
	}
	claim.Status = nextStatus
	claim.Message = message
	claim.Version++
	claim.UpdatedAt = timestamppb.New(now)
	return s.updateVolumeClaim(ctx, tx, claim)
}

func (s *Store) updateVolumeClaim(ctx context.Context, tx pgx.Tx, claim *storagev1.VolumeClaim) error {
	payload, err := marshalProto(claim)
	if err != nil {
		return err
	}
	labels, err := marshalStringMap(claim.GetLabels())
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE storage_volume_claims
		SET status = $2, labels = $3::jsonb, payload = $4::jsonb, version = $5, updated_at = $6,
			reclaim_attempt = $7, next_reclaim_at = $8, reclaim_lease_until = $9
		WHERE claim_id = $1
	`, claim.GetID(), claim.GetStatus().String(), labels, payload, claim.GetVersion(), claim.GetUpdatedAt().AsTime(),
		claim.GetReclaimAttempt(), timestampOrNil(claim.GetNextReclaimAt()), timestampOrNil(claim.GetReclaimLeaseUntil()))
	return err
}
