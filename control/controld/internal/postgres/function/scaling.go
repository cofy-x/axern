package pgfunction

import (
	"context"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) listIdleDeployments(ctx context.Context, now time.Time) ([]functionkernel.IdleDeployment, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT
			d.function_id,
			f.namespace,
			f.name,
			d.worker_service_id,
			d.scaling,
			d.desired_replicas,
			GREATEST(
				d.updated_at,
				COALESCE((
					SELECT MAX(i.completed_at)
					FROM function_invocations i
					WHERE i.function_id = d.function_id
					  AND i.revision_id = d.active_revision_id
				), d.updated_at)
			) AS idle_since
		FROM function_deployments d
		JOIN functions f ON f.function_id = d.function_id
		WHERE f.status NOT IN ($1, $2)
		  AND d.worker_service_id != ''
		  AND d.desired_replicas > 0
		  AND d.active_invocations = 0
	`, functionv1.FunctionStatus_FUNCTION_STATUS_DELETED.String(),
		functionv1.FunctionStatus_FUNCTION_STATUS_DELETING.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []functionkernel.IdleDeployment
	for rows.Next() {
		var d functionkernel.IdleDeployment
		var scalingJSON []byte
		if err := rows.Scan(&d.FunctionID, &d.Namespace, &d.FunctionName, &d.WorkerServiceID, &scalingJSON, &d.DesiredReplicas, &d.LastInvokedAt); err != nil {
			return nil, err
		}
		scaling, err := parseScalingJSON(scalingJSON)
		if err != nil || scaling == nil || scaling.GetIdleTimeout() == nil || scaling.GetIdleTimeout().AsDuration() <= 0 {
			continue
		}
		if scaling.GetMinReplicas() >= d.DesiredReplicas {
			continue
		}
		d.IdleTimeout = scaling.GetIdleTimeout().AsDuration()
		d.MinReplicas = scaling.GetMinReplicas()

		if now.Sub(d.LastInvokedAt) >= d.IdleTimeout {
			result = append(result, d)
		}
	}
	return result, rows.Err()
}

func (s *Store) recordScaleDown(ctx context.Context, functionID string, replicas int32, now time.Time) (bool, error) {
	recorded := false
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		deploymentStatus := functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_WARMING
		functionStatus := functionv1.FunctionStatus_FUNCTION_STATUS_DEPLOYING
		message := "function worker scaled to minimum replicas"
		if replicas == 0 {
			deploymentStatus = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO
			functionStatus = functionv1.FunctionStatus_FUNCTION_STATUS_READY
			message = "function worker service is scaled to zero"
		}
		tag, err := tx.Exec(ctx, `
			UPDATE function_deployments
			SET desired_replicas = $2,
				ready_replicas = LEAST(ready_replicas, $2),
				status = $3,
				updated_at = $4,
				message = $5
			WHERE function_id = $1
			  AND active_invocations = 0
			  AND desired_replicas > $2
		`, functionID, replicas, deploymentStatus.String(), now.UTC(), message)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE functions
			SET status = $2, deployment_status = $3, updated_at = $4, message = $5, version = version + 1
			WHERE function_id = $1
		`, functionID, functionStatus.String(), deploymentStatus.String(), now.UTC(), message); err != nil {
			return err
		}
		event := functionkernel.NewEvent(functionID, "", "",
			functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_SCALING_DECISION,
			"idle scale-down: worker replicas reduced to min_replicas", nil, now)
		if err := recordFunctionEventTx(ctx, tx, event); err != nil {
			return err
		}
		recorded = true
		return nil
	})
	return recorded, err
}

func parseScalingJSON(data []byte) (*functionv1.FunctionScalingSpec, error) {
	if len(data) == 0 || string(data) == "{}" || string(data) == "null" {
		return nil, nil
	}
	var scaling functionv1.FunctionScalingSpec
	if err := protojson.Unmarshal(data, &scaling); err != nil {
		return nil, err
	}
	return &scaling, nil
}
