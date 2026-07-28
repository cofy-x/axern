package pgfunction

import (
	"context"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
)

type Store struct {
	db            *postgres.DB
	notifications *invocationNotificationHub
}

func NewStore(db *postgres.DB, notificationCapacity int) *Store {
	return &Store{db: db, notifications: newInvocationNotificationHub(db.Pool(), notificationCapacity)}
}

func (s *Store) Close() {
	if s != nil && s.notifications != nil {
		s.notifications.close()
	}
}

func (s *Store) InvocationListenerReady() bool {
	return s != nil && s.notifications != nil && s.notifications.isReady()
}

func (s *Store) SaveBundle(ctx context.Context, params functionkernel.UploadBundleParams, now time.Time) (*functionv1.FunctionBundleSource, error) {
	return s.saveBundle(ctx, params, now.UTC())
}

func (s *Store) DeployFunction(ctx context.Context, params functionkernel.DeployParams, now time.Time) (*functionkernel.DeployResult, error) {
	return s.deployFunction(ctx, params, now.UTC())
}

func (s *Store) AttachWorkerService(ctx context.Context, functionID, revisionID, serviceID string, desiredReplicas int32, now time.Time) (*functionv1.Function, *functionv1.FunctionDeployment, bool, error) {
	return s.attachWorkerService(ctx, functionID, revisionID, serviceID, desiredReplicas, now.UTC())
}

func (s *Store) StartInvocation(ctx context.Context, params functionkernel.InvokeParams, now time.Time) (*functionkernel.InvocationStartResult, error) {
	return s.startInvocation(ctx, params, now.UTC())
}

func (s *Store) FinishInvocation(ctx context.Context, invocationID string, status functionv1.FunctionInvocationStatus, result *functionv1.FunctionResult, fnErr *functionv1.FunctionError, message string, now time.Time) (*functionv1.FunctionInvocation, bool, error) {
	return s.finishInvocation(ctx, invocationID, status, result, fnErr, message, now.UTC())
}

func (s *Store) GetFunction(ctx context.Context, functionID, namespace, name string) (*functionv1.Function, *functionv1.FunctionRevision, *functionv1.FunctionDeployment, bool, error) {
	return s.getFunction(ctx, functionID, namespace, name)
}

func (s *Store) ListFunctions(ctx context.Context, filter *functionv1.FunctionListFilter) ([]*functionv1.Function, string, error) {
	return s.listFunctions(ctx, filter)
}

func (s *Store) DeleteFunction(ctx context.Context, functionID, namespace, name string, now time.Time) (*functionv1.Function, bool, error) {
	return s.deleteFunction(ctx, functionID, namespace, name, now.UTC())
}

func (s *Store) GetInvocation(ctx context.Context, invocationID string) (*functionv1.FunctionInvocation, bool, error) {
	return s.getInvocation(ctx, invocationID)
}

func (s *Store) ListInvocations(ctx context.Context, filter *functionv1.FunctionInvocationListFilter) ([]*functionv1.FunctionInvocation, string, error) {
	return s.listInvocations(ctx, filter)
}

func (s *Store) ListEvents(ctx context.Context, functionID, invocationID, revisionID string, limit int32) ([]*functionv1.FunctionEvent, error) {
	return s.listEvents(ctx, functionID, invocationID, revisionID, limit)
}

func (s *Store) ListIdleDeployments(ctx context.Context, now time.Time) ([]functionkernel.IdleDeployment, error) {
	return s.listIdleDeployments(ctx, now.UTC())
}

func (s *Store) RecordScaleDown(ctx context.Context, functionID string, replicas int32, now time.Time) (bool, error) {
	return s.recordScaleDown(ctx, functionID, replicas, now.UTC())
}

func (s *Store) ClaimAsyncInvocation(ctx context.Context, owner string, leaseTTL time.Duration) (*functionkernel.AsyncInvocationClaim, bool, error) {
	return s.claimAsyncInvocation(ctx, owner, leaseTTL)
}

func (s *Store) RenewAsyncInvocation(ctx context.Context, claim *functionkernel.AsyncInvocationClaim, leaseTTL time.Duration) (bool, error) {
	return s.renewAsyncInvocation(ctx, claim, leaseTTL)
}

func (s *Store) RequeueAsyncInvocation(ctx context.Context, claim *functionkernel.AsyncInvocationClaim, delay time.Duration, message string) (bool, error) {
	return s.requeueAsyncInvocation(ctx, claim, delay, message)
}

func (s *Store) FinishAsyncInvocation(ctx context.Context, claim *functionkernel.AsyncInvocationClaim, status functionv1.FunctionInvocationStatus, result *functionv1.FunctionResult, fnErr *functionv1.FunctionError, message string) (*functionv1.FunctionInvocation, bool, error) {
	return s.finishAsyncInvocation(ctx, claim, status, result, fnErr, message)
}

func (s *Store) ExpireAsyncInvocations(ctx context.Context, limit int) (int, error) {
	return s.expireAsyncInvocations(ctx, limit)
}

func (s *Store) WaitForAsyncInvocation(ctx context.Context, safetyTimeout time.Duration) error {
	return s.notifications.wait(ctx, safetyTimeout)
}

var _ functionkernel.Store = (*Store)(nil)
