package appfunction

import (
	"context"
	"errors"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	asyncInvocationLeaseTTL      = 30 * time.Second
	asyncInvocationSafetyTimeout = time.Second
	asyncInvocationCommitTimeout = 5 * time.Second
)

func (c *Controller) RunAsyncDispatcher(ctx context.Context, owner string) {
	for ctx.Err() == nil {
		claim, ok, err := c.store.ClaimAsyncInvocation(ctx, owner, asyncInvocationLeaseTTL)
		if err != nil {
			if ctx.Err() == nil {
				logrus.WithError(err).Warn("claim async function invocation")
				_ = c.store.WaitForAsyncInvocation(ctx, asyncInvocationSafetyTimeout)
			}
			continue
		}
		if !ok {
			_ = c.store.WaitForAsyncInvocation(ctx, asyncInvocationSafetyTimeout)
			continue
		}
		c.executeAsyncClaim(ctx, claim)
	}
}

func (c *Controller) executeAsyncClaim(parent context.Context, claim *functionkernel.AsyncInvocationClaim) {
	if claim == nil || claim.DeadlineRemaining <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(parent, claim.DeadlineRemaining)
	defer cancel()
	renewCtx, stopRenew := context.WithCancel(ctx)
	renewed := make(chan bool, 1)
	go func() {
		ticker := time.NewTicker(asyncInvocationLeaseTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				renewed <- true
				return
			case <-ticker.C:
				ok, err := c.store.RenewAsyncInvocation(renewCtx, claim, asyncInvocationLeaseTTL)
				if err != nil || !ok {
					renewed <- false
					cancel()
					return
				}
			}
		}
	}()

	result, fnErr, dispatchErr := c.runClaim(ctx, claim)
	stopRenew()
	leaseHeld := <-renewed
	if !leaseHeld || parent.Err() != nil {
		return
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result = nil
		fnErr = nil
		dispatchErr = grpcstatus.Error(codes.DeadlineExceeded, "asynchronous function invocation deadline expired")
	}
	if dispatchErr != nil && retryAsyncDispatch(dispatchErr) {
		delay := asyncInvocationRetryDelay(claim.Attempt)
		requeueCtx, requeueCancel := context.WithTimeout(context.WithoutCancel(parent), asyncInvocationCommitTimeout)
		defer requeueCancel()
		if ok, err := c.store.RequeueAsyncInvocation(requeueCtx, claim, delay, dispatchErr.Error()); err != nil {
			logrus.WithError(err).WithField("invocation_id", claim.Invocation.GetID()).Warn("requeue async function invocation")
		} else if !ok {
			logrus.WithField("invocation_id", claim.Invocation.GetID()).Warn("async function invocation lease was fenced before requeue")
		}
		return
	}
	status := functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED
	message := "function invocation succeeded"
	if dispatchErr != nil || fnErr != nil {
		status = functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_FAILED
		if dispatchErr != nil && grpcstatus.Code(dispatchErr) == codes.DeadlineExceeded {
			status = functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_TIMED_OUT
		}
		fnErr = normalizeDispatchError(claim.Deployment, fnErr, dispatchErr)
		message = fnErr.GetMessage()
		result = nil
	}
	commitCtx, commitCancel := context.WithTimeout(context.WithoutCancel(parent), asyncInvocationCommitTimeout)
	defer commitCancel()
	if _, ok, err := c.store.FinishAsyncInvocation(commitCtx, claim, status, result, fnErr, message); err != nil {
		logrus.WithError(err).WithField("invocation_id", claim.Invocation.GetID()).Warn("finish async function invocation")
	} else if !ok {
		logrus.WithField("invocation_id", claim.Invocation.GetID()).Warn("async function invocation lease was fenced before completion")
	}
}

func retryAsyncDispatch(err error) bool {
	switch grpcstatus.Code(err) {
	case codes.Unavailable, codes.Internal, codes.Unknown:
		return true
	default:
		return false
	}
}

func asyncInvocationRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(1<<min(attempt-1, 4)) * 250 * time.Millisecond
	if delay > 4*time.Second {
		return 4 * time.Second
	}
	return delay
}

func (c *Controller) runClaim(ctx context.Context, claim *functionkernel.AsyncInvocationClaim) (*functionv1.FunctionResult, *functionv1.FunctionError, error) {
	prepared, err := c.prepareWorker(ctx, claim.Function, claim.Revision, claim.Deployment, time.Now())
	if err != nil {
		return nil, nil, err
	}
	claim.Deployment = prepared
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, nil, grpcstatus.Error(codes.FailedPrecondition, "asynchronous function invocation deadline is missing")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil, nil, grpcstatus.Error(codes.DeadlineExceeded, "asynchronous function invocation deadline expired")
	}
	return c.dispatchInvocation(ctx, FunctionInvokeDispatch{
		Function:   claim.Function,
		Revision:   claim.Revision,
		Deployment: prepared,
		Invocation: claim.Invocation,
		Payload:    claim.Invocation.GetPayload(),
		Timeout:    durationpb.New(remaining),
	})
}
