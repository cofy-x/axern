package pgservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *PGStore) Purge(ctx context.Context, id string, now time.Time) (string, bool, error) {
	serviceID := strings.TrimSpace(id)
	if serviceID == "" {
		return "", false, nil
	}
	var purged string
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, serviceID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
			return grpcstatus.Errorf(codes.FailedPrecondition, "service %q must be deleted before purge", current.GetID())
		}
		if current.GetDeletionStatus().GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE {
			return grpcstatus.Errorf(codes.FailedPrecondition, "service %q deletion must complete before purge", current.GetID())
		}
		hasUnreleasedAllocations, err := s.hasUnreleasedAllocationsForService(ctx, tx, current.GetID())
		if err != nil {
			return err
		}
		if hasUnreleasedAllocations {
			return grpcstatus.Errorf(codes.FailedPrecondition, "service %q still has allocations pending release", current.GetID())
		}
		allocationIDs, err := s.allAllocationIDsForService(ctx, tx, current.GetID())
		if err != nil {
			return err
		}
		if len(allocationIDs) > 0 {
			if _, err := tx.Exec(ctx, `
				DELETE FROM execution_leases
				WHERE allocation_id = ANY($1)
			`, allocationIDs); err != nil {
				return fmt.Errorf("delete service execution leases: %w", err)
			}
			if _, err := tx.Exec(ctx, `
					DELETE FROM workload_reservations
					WHERE allocation_id = ANY($1)
				`, allocationIDs); err != nil {
				return fmt.Errorf("delete service reservations: %w", err)
			}
			if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
				AllocationIDs: allocationIDs,
				Reason:        "service purged",
				ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_DELETED,
				Now:           now,
			}); err != nil {
				return fmt.Errorf("revoke service tunnel sessions: %w", err)
			}
			if _, err := tx.Exec(ctx, `
					DELETE FROM allocations
					WHERE owner_type = $1 AND owner_id = $2
			`, allocationOwnerService, current.GetID()); err != nil {
				return fmt.Errorf("delete service allocations: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM services WHERE service_id = $1`, current.GetID()); err != nil {
			return fmt.Errorf("delete service: %w", err)
		}
		if err := notifyServiceChanged(ctx, tx, current.GetID()); err != nil {
			return err
		}
		purged = current.GetID()
		return nil
	})
	return purged, purged != "", err
}

func (s *PGStore) allAllocationIDsForService(ctx context.Context, tx pgx.Tx, serviceID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT allocation_id
		FROM allocations
		WHERE owner_type = $1 AND owner_id = $2
		ORDER BY created_at ASC, allocation_id ASC
	`, allocationOwnerService, strings.TrimSpace(serviceID))
	if err != nil {
		return nil, fmt.Errorf("query service allocation ids: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var allocationID string
		if err := rows.Scan(&allocationID); err != nil {
			return nil, fmt.Errorf("scan service allocation id: %w", err)
		}
		allocationID = strings.TrimSpace(allocationID)
		if allocationID != "" {
			out = append(out, allocationID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service allocation ids: %w", err)
	}
	return out, nil
}

func (s *PGStore) hasUnreleasedAllocationsForService(ctx context.Context, tx pgx.Tx, serviceID string) (bool, error) {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM allocations
		WHERE owner_type = $1
		  AND owner_id = $2
		  AND status <> $3
	`, allocationOwnerService, strings.TrimSpace(serviceID),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String(),
	).Scan(&count); err != nil {
		return false, fmt.Errorf("count unreleased service allocations: %w", err)
	}
	return count > 0, nil
}
