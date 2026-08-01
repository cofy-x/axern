package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ReleaseWorkloadVolumeClaims serializes owner release with binding reserve by
// locking every matching claim before checking bindings or mutating ownership.
// ReserveVolumeBinding takes the same claim lock and revalidates ownership, so
// neither lock ordering can authorize a stale workload after release.
func (s *Store) ReleaseWorkloadVolumeClaims(ctx context.Context, namespace, workloadID, workloadType string, now time.Time) ([]string, error) {
	namespace = strings.TrimSpace(namespace)
	workloadID = strings.TrimSpace(workloadID)
	workloadType = strings.TrimSpace(workloadType)
	if namespace == "" || workloadID == "" || workloadType == "" {
		return nil, fmt.Errorf("release workload volume claims requires namespace, workload id, and workload type")
	}

	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT payload FROM storage_volume_claims
		WHERE namespace = $1
		  AND status <> $2
		  AND payload->>'owner_id' = $3
		  AND payload->>'owner_type' = $4
		ORDER BY claim_id
		FOR UPDATE
	`, namespace, storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String(), workloadID, workloadType)
	if err != nil {
		return nil, err
	}
	claims := make([]*storagev1.VolumeClaim, 0)
	for rows.Next() {
		claim, scanErr := scanVolumeClaim(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for _, claim := range claims {
		var bindingID string
		err := tx.QueryRow(ctx, `
			SELECT binding_id FROM storage_volume_bindings
			WHERE claim_id = $1 AND status <> $2
			ORDER BY created_at, binding_id
			LIMIT 1
		`, claim.GetID(), storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String()).Scan(&bindingID)
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
		if err == nil {
			return nil, fmt.Errorf("volume claim %q still has active binding %q", claim.GetID(), bindingID)
		}
	}

	releasedClaimIDs := make([]string, 0, len(claims))
	for _, claim := range claims {
		claim.OwnerID = ""
		claim.OwnerType = ""
		claim.Message = "volume claim owner released; backend retained"
		claim.Version++
		claim.UpdatedAt = timestamppb.New(now.UTC())
		if err := s.updateVolumeClaim(ctx, tx, claim); err != nil {
			return nil, err
		}
		releasedClaimIDs = append(releasedClaimIDs, claim.GetID())
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return releasedClaimIDs, nil
}
