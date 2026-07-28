package pgfunction

import (
	"context"
	"fmt"
	"strings"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) attachWorkerService(ctx context.Context, functionID, revisionID, serviceID string, desiredReplicas int32, now time.Time) (*functionv1.Function, *functionv1.FunctionDeployment, bool, error) {
	var (
		nextFunction   *functionv1.Function
		nextDeployment *functionv1.FunctionDeployment
		ok             bool
	)
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, _, deployment, found, err := getFunctionTx(ctx, tx, strings.TrimSpace(functionID), "", "", true)
		if err != nil || !found {
			ok = found
			return err
		}
		if current.GetActiveRevisionID() != strings.TrimSpace(revisionID) {
			return grpcstatus.Error(codes.FailedPrecondition, "function active revision changed during worker rollout")
		}
		if deployment == nil {
			return grpcstatus.Error(codes.FailedPrecondition, "function deployment is missing")
		}
		nextFunction, nextDeployment = functionkernel.MarkWorkerServiceAttached(current, deployment, serviceID, desiredReplicas, now)
		if err := updateFunctionTx(ctx, tx, nextFunction); err != nil {
			return err
		}
		if err := upsertDeploymentTx(ctx, tx, nextDeployment); err != nil {
			return err
		}
		if err := recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(nextFunction.GetID(), "", revisionID, functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_SCALING_DECISION, "function worker rollout started", map[string]string{
			"worker_service_id": strings.TrimSpace(serviceID),
		}, now)); err != nil {
			return err
		}
		ok = true
		return nil
	})
	if err != nil {
		return nil, nil, false, fmt.Errorf("attach function worker service: %w", err)
	}
	return nextFunction, nextDeployment, ok, nil
}
