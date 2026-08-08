package reservation

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type quotaAdmissionRejectedEvent struct {
	Namespace     string
	OwnerType     string
	OwnerID       string
	EnvironmentID string
	Evaluation    resourcekernel.QuotaEvaluation
	Message       string
	CreatedAt     time.Time
}

func insertQuotaAdmissionRejectedEvent(ctx context.Context, tx pgx.Tx, event quotaAdmissionRejectedEvent) error {
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	evaluation := event.Evaluation
	if _, err := tx.Exec(ctx, `
		INSERT INTO namespace_quota_events (
			event_id, namespace, event_type, workload_type, workload_id, environment_id, reason,
			requested_cpu_milli, reserved_cpu_milli, cpu_milli_limit, available_cpu_milli,
			requested_memory_bytes, reserved_memory_bytes, memory_bytes_limit, available_memory_bytes,
			requested_ephemeral_storage_bytes, reserved_ephemeral_storage_bytes, ephemeral_storage_bytes_limit, available_ephemeral_storage_bytes,
			message, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11,
			$12, $13, $14, $15,
			$16, $17, $18, $19,
			$20, $21
		)
	`,
		"quotaevt-"+uuid.NewString(),
		environmentkernel.NormalizeNamespace(event.Namespace),
		string(resourcekernel.QuotaEventTypeAdmissionRejected),
		event.OwnerType,
		event.OwnerID,
		event.EnvironmentID,
		string(resourcekernel.QuotaEventReasonForEvaluation(evaluation)),
		evaluation.CPU.Requested,
		evaluation.CPU.Used,
		nullableInt64(evaluation.CPU.Limit),
		nullableInt64(evaluation.CPU.Available),
		evaluation.Memory.Requested,
		evaluation.Memory.Used,
		nullableInt64(evaluation.Memory.Limit),
		nullableInt64(evaluation.Memory.Available),
		evaluation.EphemeralStorage.Requested,
		evaluation.EphemeralStorage.Used,
		nullableInt64(evaluation.EphemeralStorage.Limit),
		nullableInt64(evaluation.EphemeralStorage.Available),
		event.Message,
		createdAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert namespace quota event: %w", err)
	}
	return nil
}

func nullableInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}
