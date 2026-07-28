package managedworker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/redact"
	"github.com/cofy-x/axern/lib/go/agentprofile"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type preflightFailure struct {
	report *rolloutv1.PreflightReport
	err    error
}

func (e *preflightFailure) Error() string { return e.err.Error() }
func (e *preflightFailure) Unwrap() error { return e.err }

func (w Worker) execute(parent context.Context, work *workerrolloutv1.WorkItem, leaseToken string) error {
	workOutput, err := os.MkdirTemp(w.config.OutputDir, "work-")
	if err != nil {
		return fmt.Errorf("create worker output directory: %w", err)
	}
	defer os.RemoveAll(workOutput)
	config := w.config
	config.OutputDir = workOutput
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	renewDone := make(chan struct{})
	var leaseErr error
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		defer close(renewDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				response, err := w.client.RenewWorkLease(ctx, &workerrolloutv1.RenewWorkLeaseRequest{
					WorkID:     work.GetID(),
					LeaseToken: leaseToken,
				})
				if err != nil {
					leaseErr = err
					cancel()
					return
				}
				if response.GetCancelRequested() {
					cancel()
					return
				}
			}
		}
	}()
	switch work.GetKind() {
	case workerrolloutv1.WorkKind_WORK_KIND_PROFILE_DOCTOR:
		err = w.doctor(ctx, work, leaseToken)
	case workerrolloutv1.WorkKind_WORK_KIND_PLAN:
		err = w.plan(ctx, work, leaseToken, config)
	case workerrolloutv1.WorkKind_WORK_KIND_EPISODE:
		err = w.episode(ctx, work, leaseToken, config)
	default:
		err = status.Errorf(codes.FailedPrecondition, "unsupported work kind %s", work.GetKind())
	}
	cancel()
	<-renewDone
	if err != nil && leaseErr != nil {
		err = fmt.Errorf("renew work lease: %w", leaseErr)
	}
	if err == nil {
		return nil
	}
	reportCtx, reportCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer reportCancel()
	safeMessage := redact.String(err.Error())
	code := "AXRUN_WORKER"
	var preflight *rolloutv1.PreflightReport
	var preflightErr *preflightFailure
	if errors.As(err, &preflightErr) {
		code = "PREFLIGHT_REJECTED"
		preflight = preflightErr.report
	}
	if status.Code(err) == codes.ResourceExhausted {
		code = "BUDGET_EXHAUSTED"
	}
	retriable := isRetriable(err)
	if parent.Err() != nil {
		// A worker deployment shutdown is an infrastructure interruption. The
		// durable work item must remain retryable on another worker.
		retriable = true
	}
	_, failErr := w.client.FailWork(reportCtx, &workerrolloutv1.FailWorkRequest{
		WorkID:     work.GetID(),
		LeaseToken: leaseToken,
		Code:       code,
		Message:    safeMessage,
		Retriable:  retriable,
		Preflight:  preflight,
	})
	if failErr != nil {
		return fmt.Errorf("work failed: %s; report failure: %w", safeMessage, failErr)
	}
	return errors.New(safeMessage)
}

func isRetriable(err error) bool {
	var probeErr *agentprofile.ProbeError
	if errors.As(err, &probeErr) {
		return probeErr.Result.Retryable
	}
	var temporary interface{ Temporary() bool }
	if errors.As(err, &temporary) && temporary.Temporary() {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted:
		return true
	default:
		return false
	}
}
