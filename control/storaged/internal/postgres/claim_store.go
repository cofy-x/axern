package postgres

import (
	"context"
	"fmt"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

func (s *Store) CreateVolumeClaim(ctx context.Context, claim *storagev1.VolumeClaim) (*storagev1.VolumeClaim, error) {
	payload, err := marshalProto(claim)
	if err != nil {
		return nil, err
	}
	labels, err := marshalStringMap(claim.GetLabels())
	if err != nil {
		return nil, err
	}
	_, err = s.db.Pool().Exec(ctx, `
		INSERT INTO storage_volume_claims (namespace, name, claim_id, class_name, status, labels, payload, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10)
	`, claim.GetNamespace(), claim.GetName(), claim.GetID(), claim.GetClassName(), claim.GetStatus().String(), labels, payload, claim.GetVersion(), claim.GetCreatedAt().AsTime(), claim.GetUpdatedAt().AsTime())
	if err != nil {
		return nil, fmt.Errorf("create volume claim %q/%q: %w", claim.GetNamespace(), claim.GetName(), err)
	}
	return proto.Clone(claim).(*storagev1.VolumeClaim), nil
}

func (s *Store) GetVolumeClaim(ctx context.Context, namespace, name string) (*storagev1.VolumeClaim, bool, error) {
	row := s.db.Pool().QueryRow(ctx, `
		SELECT payload FROM storage_volume_claims
		WHERE namespace = $1 AND name = $2 AND status <> $3
		ORDER BY created_at DESC LIMIT 1
	`, strings.TrimSpace(namespace), strings.TrimSpace(name), storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String())
	out, err := scanVolumeClaim(row)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (s *Store) ListVolumeClaims(ctx context.Context, filter *storagev1.VolumeClaimListFilter) ([]*storagev1.VolumeClaim, error) {
	rows, err := s.db.Pool().Query(ctx, `SELECT payload FROM storage_volume_claims ORDER BY namespace, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storagev1.VolumeClaim
	for rows.Next() {
		claim, err := scanVolumeClaim(rows)
		if err != nil {
			return nil, err
		}
		if claimMatchesFilter(claim, filter) {
			out = append(out, claim)
		}
	}
	return out, rows.Err()
}

func (s *Store) UpdateVolumeClaim(ctx context.Context, namespace, name string, expectedVersion int64, mutate func(*storagev1.VolumeClaim) error) (*storagev1.VolumeClaim, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	current, err := scanVolumeClaim(tx.QueryRow(ctx, `
		SELECT payload FROM storage_volume_claims
		WHERE namespace = $1 AND name = $2 AND status <> $3
		ORDER BY created_at DESC LIMIT 1 FOR UPDATE
	`, namespace, name, storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String()))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("volume claim %q/%q not found", namespace, name)
		}
		return nil, err
	}
	if expectedVersion > 0 && current.GetVersion() != expectedVersion {
		return nil, fmt.Errorf("volume claim %q/%q version mismatch: got %d, want %d", namespace, name, current.GetVersion(), expectedVersion)
	}
	next := proto.Clone(current).(*storagev1.VolumeClaim)
	if err := mutate(next); err != nil {
		return nil, err
	}
	if err := s.updateVolumeClaim(ctx, tx, next); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return proto.Clone(next).(*storagev1.VolumeClaim), nil
}
