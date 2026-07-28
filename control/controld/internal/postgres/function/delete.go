package pgfunction

import (
	"context"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) deleteFunction(ctx context.Context, functionID, namespace, name string, now time.Time) (*functionv1.Function, bool, error) {
	var deleted *functionv1.Function
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, _, currentDeployment, ok, err := getFunctionTx(ctx, tx, functionID, namespace, name, true)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if current.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_DELETED {
			deleted = current
			return nil
		}
		if currentDeployment != nil && currentDeployment.GetActiveInvocations() > 0 {
			return grpcstatus.Error(codes.FailedPrecondition, "function cannot be deleted while invocations are active")
		}
		next := functionkernel.MarkDeleted(current, now)
		deployment := functionkernel.MarkDeploymentDeleted(
			currentDeployment,
			next.GetID(),
			next.GetActiveRevisionID(),
			functionScaling(next.GetSpec()),
			now,
		)
		if err := updateFunctionTx(ctx, tx, next); err != nil {
			return err
		}
		if err := upsertDeploymentTx(ctx, tx, deployment); err != nil {
			return err
		}
		if err := recordFunctionEventTx(ctx, tx, functionkernel.NewEvent(next.GetID(), "", next.GetActiveRevisionID(), functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_CLEANUP, "function deleted", nil, now)); err != nil {
			return err
		}
		deleted = next
		return nil
	})
	return deleted, deleted != nil, err
}
