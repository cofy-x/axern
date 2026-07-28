package reservation

import (
	"context"
	"fmt"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type WorkloadReservation struct {
	AllocationID string
	Namespace    string
	OwnerType    string
	OwnerID      string
	NodeID       string
	Requests     *commonv1.ResourceQuantity
	CreatedAt    time.Time
}

func InsertWorkloadReservation(ctx context.Context, tx pgx.Tx, reservation WorkloadReservation) error {
	requests := reservation.Requests
	if requests == nil {
		requests = &commonv1.ResourceQuantity{}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workload_reservations (
			reservation_id, allocation_id, namespace, owner_type, owner_id, node_id,
			cpu_milli, memory_bytes, created_at, released_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL)
	`, "resv-"+uuid.NewString(),
		reservation.AllocationID,
		environmentkernel.NormalizeNamespace(reservation.Namespace),
		reservation.OwnerType,
		reservation.OwnerID,
		reservation.NodeID,
		requests.GetCpuMilli(),
		requests.GetMemoryBytes(),
		reservation.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert workload reservation: %w", err)
	}
	return nil
}
