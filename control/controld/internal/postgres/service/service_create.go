package pgservice

import (
	"context"
	"fmt"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5"
)

func (s *PGStore) Create(ctx context.Context, params servicekernel.CreateParams, now time.Time) (*servicev1.Service, error) {
	policy, err := servicekernel.ValidateAndNormalizeRolloutPolicy(params.RolloutPolicy)
	if err != nil {
		return nil, err
	}
	readiness, err := servicekernel.ValidateAndNormalizeReadinessProbe(params.ReadinessProbe)
	if err != nil {
		return nil, err
	}
	liveness, err := servicekernel.ValidateAndNormalizeLivenessProbe(params.LivenessProbe)
	if err != nil {
		return nil, err
	}
	service := servicekernel.NewService(params.Namespace, params.EnvironmentID, params.Replicas, params.Config, params.Labels, policy, readiness, liveness, now)
	configJSON, err := marshalProtoJSON(service.GetConfig())
	if err != nil {
		return nil, err
	}
	rolloutPolicyJSON, err := marshalProtoJSON(service.GetRolloutPolicy())
	if err != nil {
		return nil, err
	}
	readinessProbeJSON, err := marshalProtoJSON(service.GetReadinessProbe())
	if err != nil {
		return nil, err
	}
	livenessProbeJSON, err := marshalProtoJSON(service.GetLivenessProbe())
	if err != nil {
		return nil, err
	}
	labelsJSON, err := marshalJSONMap(service.GetLabels())
	if err != nil {
		return nil, err
	}
	allocationIDsJSON, err := marshalStringSlice(nil)
	if err != nil {
		return nil, err
	}
	if err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := pgnamespace.Ensure(ctx, tx, service.GetNamespace()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO services (
				service_id, namespace, environment_id, replicas, ready_replicas, unhealthy_replicas, rollout_policy, readiness_probe, liveness_probe, status, config,
				allocation_ids, labels, version, created_at, updated_at, message, diagnostic_code
			) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb, $10, $11::jsonb, $12::jsonb, $13::jsonb, $14, $15, $16, $17, $18)
		`, service.GetID(), service.GetNamespace(), service.GetEnvironmentID(), service.GetReplicas(), service.GetReadyReplicas(), service.GetUnhealthyReplicas(), rolloutPolicyJSON, readinessProbeJSON, livenessProbeJSON, service.GetStatus().String(), configJSON, allocationIDsJSON, labelsJSON, service.GetVersion(), now.UTC(), now.UTC(), service.GetMessage(), service.GetDiagnosticCode().String()); err != nil {
			return fmt.Errorf("insert service: %w", err)
		}
		return notifyServiceChanged(ctx, tx, service.GetID())
	}); err != nil {
		return nil, err
	}
	return servicekernel.CloneService(service), nil
}
