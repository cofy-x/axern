package namespace

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	DefaultQuotaEventListLimit = 50
	MaxQuotaEventListLimit     = 200
)

func (s *Store) ListEvents(ctx context.Context, namespace string, limit int) ([]*quotav1.NamespaceQuotaEvent, error) {
	normalized := normalizeNamespace(namespace)
	limit = normalizeQuotaEventLimit(limit)
	rows, err := s.db.Pool().Query(ctx, `
		SELECT event_id, namespace, event_type, workload_type, workload_id, environment_id, reason,
		       requested_cpu_milli, reserved_cpu_milli, cpu_milli_limit, available_cpu_milli,
		       requested_memory_bytes, reserved_memory_bytes, memory_bytes_limit, available_memory_bytes,
		       message, created_at
		FROM namespace_quota_events
		WHERE namespace = $1
		ORDER BY created_at DESC, event_id DESC
		LIMIT $2
	`, normalized, limit)
	if err != nil {
		return nil, fmt.Errorf("list namespace quota events: %w", err)
	}
	defer rows.Close()
	events := make([]*quotav1.NamespaceQuotaEvent, 0)
	for rows.Next() {
		event, err := scanQuotaEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list namespace quota events: %w", err)
	}
	return events, nil
}

func normalizeQuotaEventLimit(limit int) int {
	if limit <= 0 {
		return DefaultQuotaEventListLimit
	}
	if limit > MaxQuotaEventListLimit {
		return MaxQuotaEventListLimit
	}
	return limit
}

func scanQuotaEvent(row quotaScanner) (*quotav1.NamespaceQuotaEvent, error) {
	var (
		event                           quotav1.NamespaceQuotaEvent
		eventType, workloadType, reason string
		cpuLimit, cpuAvailable          sql.NullInt64
		memoryLimit, memoryAvailable    sql.NullInt64
		createdAt                       sql.NullTime
	)
	if err := row.Scan(
		&event.ID,
		&event.Namespace,
		&eventType,
		&workloadType,
		&event.WorkloadID,
		&event.EnvironmentID,
		&reason,
		&event.RequestedCpuMilli,
		&event.ReservedCpuMilli,
		&cpuLimit,
		&cpuAvailable,
		&event.RequestedMemoryBytes,
		&event.ReservedMemoryBytes,
		&memoryLimit,
		&memoryAvailable,
		&event.Message,
		&createdAt,
	); err != nil {
		return nil, err
	}
	event.Type = quotaEventType(eventType)
	event.WorkloadType = quotaEventWorkloadType(workloadType)
	event.Reason = quotaEventReason(reason)
	event.CpuMilliLimit = optionalEventInt64(cpuLimit)
	event.AvailableCpuMilli = optionalEventInt64(cpuAvailable)
	event.MemoryBytesLimit = optionalEventInt64(memoryLimit)
	event.AvailableMemoryBytes = optionalEventInt64(memoryAvailable)
	if createdAt.Valid {
		event.CreatedAt = timestamppb.New(createdAt.Time)
	}
	return &event, nil
}

func quotaEventType(value string) quotav1.NamespaceQuotaEventType {
	switch strings.TrimSpace(value) {
	case string(resourcekernel.QuotaEventTypeAdmissionRejected):
		return quotav1.NamespaceQuotaEventType_NAMESPACE_QUOTA_EVENT_TYPE_ADMISSION_REJECTED
	default:
		return quotav1.NamespaceQuotaEventType_NAMESPACE_QUOTA_EVENT_TYPE_UNSPECIFIED
	}
}

func quotaEventWorkloadType(value string) quotav1.NamespaceQuotaEventWorkloadType {
	switch strings.TrimSpace(value) {
	case string(resourcekernel.QuotaEventWorkloadRun):
		return quotav1.NamespaceQuotaEventWorkloadType_NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_RUN
	case string(resourcekernel.QuotaEventWorkloadService):
		return quotav1.NamespaceQuotaEventWorkloadType_NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_SERVICE
	default:
		return quotav1.NamespaceQuotaEventWorkloadType_NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_UNSPECIFIED
	}
}

func quotaEventReason(value string) quotav1.NamespaceQuotaEventReason {
	switch strings.TrimSpace(value) {
	case string(resourcekernel.QuotaEventReasonInsufficientCPU):
		return quotav1.NamespaceQuotaEventReason_NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_CPU
	case string(resourcekernel.QuotaEventReasonInsufficientMemory):
		return quotav1.NamespaceQuotaEventReason_NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_MEMORY
	case string(resourcekernel.QuotaEventReasonInsufficientCPUMemory):
		return quotav1.NamespaceQuotaEventReason_NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_CPU_MEMORY
	default:
		return quotav1.NamespaceQuotaEventReason_NAMESPACE_QUOTA_EVENT_REASON_UNSPECIFIED
	}
}

func optionalEventInt64(value sql.NullInt64) *wrapperspb.Int64Value {
	if !value.Valid {
		return nil
	}
	return wrapperspb.Int64(value.Int64)
}
