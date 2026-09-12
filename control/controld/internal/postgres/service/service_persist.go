package pgservice

import (
	"context"
	"fmt"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5"
)

func (s *PGStore) persistService(ctx context.Context, tx pgx.Tx, next *servicev1.Service, now time.Time) error {
	configJSON, err := marshalProtoJSON(next.GetConfig())
	if err != nil {
		return err
	}
	rolloutPolicyJSON, err := marshalProtoJSON(servicekernel.CloneRolloutPolicy(next.GetRolloutPolicy()))
	if err != nil {
		return err
	}
	readinessProbeJSON, err := marshalProtoJSON(servicekernel.CloneReadinessProbe(next.GetReadinessProbe()))
	if err != nil {
		return err
	}
	livenessProbeJSON, err := marshalProtoJSON(servicekernel.CloneLivenessProbe(next.GetLivenessProbe()))
	if err != nil {
		return err
	}
	allocationIDsJSON, err := marshalStringSlice(next.GetAllocationIds())
	if err != nil {
		return err
	}
	labelsJSON, err := marshalJSONMap(next.GetLabels())
	if err != nil {
		return err
	}
	deletionStatusJSON, err := marshalProtoJSON(servicekernel.CloneDeletionStatus(next.GetDeletionStatus()))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE services
		SET environment_id = $2, replicas = $3, ready_replicas = $4, unhealthy_replicas = $5, rollout_policy = $6::jsonb, readiness_probe = $7::jsonb, liveness_probe = $8::jsonb, status = $9, config = $10::jsonb, allocation_ids = $11::jsonb,
			labels = $12::jsonb, version = $13, updated_at = $14, message = $15, diagnostic_code = $16, deletion_status = $17::jsonb
		WHERE service_id = $1
	`, next.GetID(), next.GetEnvironmentID(), next.GetReplicas(), next.GetReadyReplicas(), next.GetUnhealthyReplicas(), rolloutPolicyJSON, readinessProbeJSON, livenessProbeJSON, next.GetStatus().String(), configJSON, allocationIDsJSON, labelsJSON, next.GetVersion(), now.UTC(), next.GetMessage(), next.GetDiagnosticCode().String(), deletionStatusJSON); err != nil {
		return fmt.Errorf("persist service: %w", err)
	}
	return notifyServiceChanged(ctx, tx, next.GetID())
}
