package pgallocation

import (
	"context"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

type capabilityVerificationExecutor interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// RecordCapabilityVerification persists both the structured conditions and
// the concrete evidence actually admitted by axnoded after runtime creation.
func RecordCapabilityVerification(ctx context.Context, executor capabilityVerificationExecutor, allocationID string, admission *allocationkernel.CapabilityAdmission, now time.Time) error {
	if admission == nil {
		admission = &allocationkernel.CapabilityAdmission{}
	}
	conditionsJSON, err := protojson.Marshal(&capabilityv1.CapabilityConditionSet{Conditions: admission.Conditions})
	if err != nil {
		return fmt.Errorf("marshal allocation capability conditions: %w", err)
	}
	evidenceJSON, err := protojson.Marshal(&capabilityv1.CapabilityDependencySet{Dependencies: admission.Dependencies})
	if err != nil {
		return fmt.Errorf("marshal admitted capability evidence: %w", err)
	}
	var updated int
	err = executor.QueryRow(ctx, `
		WITH updated_allocation AS (
			UPDATE allocations
			SET capability_conditions = $2::jsonb,
				admitted_capability_dependencies = $3::jsonb,
				version = version + 1,
				updated_at = $4
			WHERE allocation_id = $1
			RETURNING allocation_id
		), updated_run AS (
			UPDATE runs
			SET capability_conditions = $2::jsonb,
				version = version + 1,
				updated_at = $4
			FROM updated_allocation
			WHERE runs.allocation_id = updated_allocation.allocation_id
			RETURNING runs.allocation_id
		)
		SELECT COUNT(*) FROM updated_allocation
	`, strings.TrimSpace(allocationID), string(conditionsJSON), string(evidenceJSON), now.UTC()).Scan(&updated)
	if err != nil {
		return fmt.Errorf("persist allocation capability verification: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("allocation %q not found while persisting capability verification", allocationID)
	}
	return nil
}
