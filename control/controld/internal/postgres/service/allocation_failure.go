package pgservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *PGStore) MarkAllocationCreateFailed(ctx context.Context, serviceID, allocationID, message string, now time.Time) (*servicev1.Service, error) {
	var service *servicev1.Service
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE allocations
			SET status = $2, message = $3, version = version + 1, updated_at = $4
			WHERE allocation_id = $1 AND owner_type = $5 AND owner_id = $6
		`, strings.TrimSpace(allocationID), commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(), strings.TrimSpace(message), now.UTC(), allocationOwnerService, strings.TrimSpace(serviceID)); err != nil {
			return fmt.Errorf("mark service allocation failed: %w", err)
		}
		if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
			AllocationIDs: []string{strings.TrimSpace(allocationID)},
			Reason:        "service allocation failed",
			ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED,
			Now:           now,
		}); err != nil {
			return err
		}
		if err := pgreservation.ReleaseAllocation(ctx, tx, allocationID, now); err != nil {
			return err
		}
		current, err := scanService(tx.QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1 FOR UPDATE`, strings.TrimSpace(serviceID)))
		if err != nil {
			return err
		}
		next := servicekernel.CloneService(current)
		next.AllocationIds = removeAllocationID(next.GetAllocationIds(), allocationID)
		if next.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETING && next.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
			next.Status = servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED
			next.Message = strings.TrimSpace(message)
		}
		next.Version++
		next.UpdatedAt = timestamppb.New(now)
		if err := s.persistService(ctx, tx, next, now); err != nil {
			return err
		}
		applyServiceDiagnostics(next)
		service = next
		return nil
	})
	return service, err
}
