package reservation

import (
	"context"
	"fmt"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	"github.com/cofy-x/axern/lib/go/memorybudget"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
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

type MemoryAdmissionEvidence struct {
	AllocationID string
	Attempt      int64
	NodeID       string
	Resources    *commonv1.ResourceSpec
	Summary      *nodev1.NodeSummary
	AdmittedAt   time.Time
}

func InsertMemoryAdmissionEvidence(ctx context.Context, tx pgx.Tx, evidence MemoryAdmissionEvidence) error {
	if err := validateMemoryAdmissionEvidence(evidence); err != nil {
		return err
	}
	requests := evidence.Resources.GetRequests()
	limits := evidence.Resources.GetLimits()
	budget := evidence.Summary.GetMemoryBudget()
	payload, err := marshalMemoryAdmissionBudget(budget)
	if err != nil {
		return err
	}
	summaryTime := evidence.AdmittedAt.UTC()
	if evidence.Summary.GetCollectedAt() != nil {
		summaryTime = evidence.Summary.GetCollectedAt().AsTime()
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO allocation_memory_admission_evidence (
			allocation_id, allocation_attempt, node_id,
			sandbox_memory_request_bytes, sandbox_memory_limit_bytes,
			node_memory_budget, summary_collected_at, node_local_commitment_bytes, admitted_at
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
	`, evidence.AllocationID, evidence.Attempt, evidence.NodeID,
		requests.GetMemoryBytes(), limits.GetMemoryBytes(), payload, summaryTime,
		budget.GetLocalCommitmentBytes(), evidence.AdmittedAt.UTC()); err != nil {
		return fmt.Errorf("insert allocation memory admission evidence: %w", err)
	}
	return nil
}

func validateMemoryAdmissionEvidence(evidence MemoryAdmissionEvidence) error {
	if evidence.AllocationID == "" || evidence.NodeID == "" {
		return fmt.Errorf("memory admission evidence allocation and node identity are required")
	}
	if evidence.Attempt <= 0 {
		return fmt.Errorf("memory admission evidence attempt must be positive")
	}
	if evidence.AdmittedAt.IsZero() {
		return fmt.Errorf("memory admission evidence time is required")
	}
	requests := evidence.Resources.GetRequests()
	limits := evidence.Resources.GetLimits()
	if requests.GetMemoryBytes() < 0 || limits.GetMemoryBytes() < 0 ||
		(limits.GetMemoryBytes() > 0 && requests.GetMemoryBytes() > limits.GetMemoryBytes()) {
		return fmt.Errorf("memory admission evidence request and limit are inconsistent")
	}
	budget := evidence.Summary.GetMemoryBudget()
	if err := memorybudget.ValidateSummary(evidence.Summary, evidence.AdmittedAt); err != nil {
		return fmt.Errorf("sandbox admission requires a valid node memory budget: %w", err)
	}
	if budget.GetSystemReserveExhausted() {
		return fmt.Errorf("sandbox admission requires a healthy node memory budget")
	}
	return nil
}

func marshalMemoryAdmissionBudget(budget *nodev1.NodeMemoryBudget) ([]byte, error) {
	if budget == nil {
		return nil, fmt.Errorf("marshal node memory admission budget: budget is required")
	}
	payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(budget)
	if err != nil {
		return nil, fmt.Errorf("marshal node memory admission budget: %w", err)
	}
	return payload, nil
}

func InsertWorkloadReservation(ctx context.Context, tx pgx.Tx, reservation WorkloadReservation) error {
	requests := reservation.Requests
	if requests == nil {
		requests = &commonv1.ResourceQuantity{}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workload_reservations (
			reservation_id, allocation_id, namespace, owner_type, owner_id, node_id,
			cpu_milli, sandbox_memory_request_bytes, ephemeral_storage_bytes, created_at, released_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL)
	`, "resv-"+uuid.NewString(),
		reservation.AllocationID,
		environmentkernel.NormalizeNamespace(reservation.Namespace),
		reservation.OwnerType,
		reservation.OwnerID,
		reservation.NodeID,
		requests.GetCpuMilli(),
		requests.GetMemoryBytes(),
		requests.GetEphemeralStorageBytes(),
		reservation.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert workload reservation: %w", err)
	}
	return nil
}
