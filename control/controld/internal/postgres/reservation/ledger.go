package reservation

import (
	"context"
	"fmt"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/jackc/pgx/v5"
)

type Reservation struct {
	AllocationID string
	NodeID       string
	Requests     *commonv1.ResourceQuantity
	CreatedAt    time.Time
}

func InsertReservation(ctx context.Context, tx pgx.Tx, reservation Reservation) error {
	requests := reservation.Requests
	if requests == nil {
		requests = &commonv1.ResourceQuantity{}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO reservations (
			allocation_id, node_id,
			cpu_milli, sandbox_memory_request_bytes, ephemeral_storage_bytes, created_at, released_at
		) VALUES ($1, $2, $3, $4, $5, $6, NULL)
	`, reservation.AllocationID,
		reservation.NodeID,
		requests.GetCpuMilli(),
		requests.GetMemoryBytes(),
		requests.GetEphemeralStorageBytes(),
		reservation.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert reservation: %w", err)
	}
	return nil
}
