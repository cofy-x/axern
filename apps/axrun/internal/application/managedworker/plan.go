package managedworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/apps/axrun/internal/redact"
	"github.com/cofy-x/axern/lib/go/agentprofile"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
)

type preflightResult struct {
	Report             *rolloutv1.PreflightReport
	UsageReservationID string
}

func (w Worker) plan(ctx context.Context, work *workerrolloutv1.WorkItem, leaseToken string, config Config) error {
	params, err := paramsFromWork(ctx, work, config)
	if err != nil {
		return err
	}
	plan, err := (approllout.Service{}).PlanForControl(params)
	if err != nil {
		if !isRetriable(err) {
			check := &rolloutv1.PreflightCheck{
				Kind:    rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_TASK_SET,
				Status:  rolloutv1.PreflightCheckStatus_PREFLIGHT_CHECK_STATUS_FAIL,
				Code:    "taskset_planning_rejected",
				Message: redact.String(err.Error()),
			}
			return &preflightFailure{report: &rolloutv1.PreflightReport{Checks: []*rolloutv1.PreflightCheck{check}}, err: err}
		}
		return err
	}
	tasks := make([]*workerrolloutv1.PlannedTask, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		tasks = append(tasks, &workerrolloutv1.PlannedTask{
			TaskID:     task.TaskID,
			TaskDigest: task.TaskDigest,
			TaskJson:   task.TaskJSON,
		})
	}
	preflight, err := w.preflight(ctx, work, leaseToken, plan)
	if err != nil {
		return err
	}
	_, err = w.client.CompletePlan(ctx, &workerrolloutv1.CompletePlanRequest{
		WorkID:             work.GetID(),
		LeaseToken:         leaseToken,
		ResultDigest:       plan.ResultDigest,
		SourceDigest:       plan.SourceDigest,
		DescriptorDigest:   plan.DescriptorDigest,
		PlanJson:           plan.PlanJSON,
		Tasks:              tasks,
		Preflight:          preflight.Report,
		UsageReservationID: preflight.UsageReservationID,
	})
	return err
}

func (w Worker) preflight(ctx context.Context, work *workerrolloutv1.WorkItem, leaseToken string, plan approllout.ControlPlan) (preflightResult, error) {
	spec := work.GetRollout().GetSpec()
	taskCount := len(plan.Tasks)
	report := &rolloutv1.PreflightReport{
		SourceDigest:      plan.SourceDigest,
		DescriptorDigest:  plan.DescriptorDigest,
		TaskCount:         int32(taskCount),
		EpisodeCount:      int32(taskCount) * spec.GetExecution().GetAttempts(),
		AgentBundleDigest: spec.GetAgent().GetImage(),
	}
	for _, payload := range plan.Payloads {
		report.PayloadVariants = append(report.PayloadVariants, &rolloutv1.PayloadVariant{
			Format:    payload.Format,
			Digest:    payload.Digest,
			MediaType: payload.MediaType,
		})
	}
	pass := func(kind rolloutv1.PreflightCheckKind, message string) {
		report.Checks = append(report.Checks, &rolloutv1.PreflightCheck{
			Kind:    kind,
			Status:  rolloutv1.PreflightCheckStatus_PREFLIGHT_CHECK_STATUS_PASS,
			Code:    "ok",
			Message: message,
		})
	}
	pass(rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_TASK_SET, "immutable TaskSet descriptor resolved")
	pass(rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_SELECTION, "task selection frozen")
	pass(rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_RUNTIME, "runtime class accepted by planning worker")
	pass(rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_WORKER_CAPABILITY, "planning worker capability matched")
	if spec.GetAgent().GetName() == "command" {
		pass(rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_BUDGET, "command rollout has no provider usage budget")
		return preflightResult{Report: report}, nil
	}
	resolved, err := w.resolveAgentProfile(ctx, work, leaseToken)
	if err != nil {
		return preflightResult{}, err
	}
	controlProfile := resolved.Snapshot.GetProfile()
	report.ProfileID = controlProfile.GetID()
	report.ProfileName = controlProfile.GetName()
	report.ProfileVersion = controlProfile.GetVersion()
	report.CredentialVersion = controlProfile.GetCredentialVersion()
	pass(rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_PROFILE, "frozen agent profile and credential resolved")
	if report.AgentBundleDigest != "" {
		pass(rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_AGENT_BUNDLE, "immutable agent bundle accepted")
	}
	reserveInput, reserveOutput := int64(128), int64(1)
	reserveCost := int64(0)
	ceilingCost := w.estimateCost(spec.GetModel(), &domain.UsageMetrics{
		InputTokens:  int(reserveInput),
		OutputTokens: int(reserveOutput),
	})
	if spec.GetBudget().GetMaxCostMicrousd() > 0 && ceilingCost == nil {
		check := &rolloutv1.PreflightCheck{
			Kind:    rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_BUDGET,
			Status:  rolloutv1.PreflightCheckStatus_PREFLIGHT_CHECK_STATUS_FAIL,
			Code:    "unknown_model_price",
			Message: "cost budget requires configured model pricing",
		}
		report.Checks = append(report.Checks, check)
		return preflightResult{}, &preflightFailure{report: report, err: errors.New(check.Message)}
	}
	if ceilingCost != nil {
		reserveCost = int64(math.Ceil(ceilingCost.Amount * 1_000_000))
	}
	reservationID := preflightReservationID(work, leaseToken)
	if _, err := w.client.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{
		WorkID:          work.GetID(),
		LeaseToken:      leaseToken,
		ReservationID:   reservationID,
		MaxTokens:       reserveInput + reserveOutput,
		MaxCostMicrousd: reserveCost,
	}); err != nil {
		return preflightResult{}, err
	}
	settled := false
	defer func() {
		if !settled {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = w.client.ReleaseUsage(releaseCtx, &workerrolloutv1.ReleaseUsageRequest{
				WorkID:        work.GetID(),
				LeaseToken:    leaseToken,
				ReservationID: reservationID,
			})
		}
	}()
	result, probeErr := agentprofile.Probe(ctx, agentprofile.ProbeRequest{Profile: resolved.Runtime, Model: spec.GetModel()})
	input, output, estimated := result.InputTokens, result.OutputTokens, !result.UsageReported
	if estimated {
		input, output = reserveInput, reserveOutput
	}
	actualCost := int64(0)
	if cost := w.estimateCost(spec.GetModel(), &domain.UsageMetrics{
		InputTokens:  int(input),
		OutputTokens: int(output),
	}); cost != nil {
		actualCost = int64(math.Ceil(cost.Amount * 1_000_000))
	}
	if _, err := w.client.CommitUsage(ctx, &workerrolloutv1.CommitUsageRequest{
		WorkID:        work.GetID(),
		LeaseToken:    leaseToken,
		ReservationID: reservationID,
		InputTokens:   input,
		OutputTokens:  output,
		CostMicrousd:  actualCost,
	}); err != nil {
		return preflightResult{}, err
	}
	settled = true
	report.Usage = &rolloutv1.PreflightUsage{
		InputTokens:  input,
		OutputTokens: output,
		CostMicrousd: actualCost,
		Estimated:    estimated,
	}
	providerCheck := &rolloutv1.PreflightCheck{
		Kind:      rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_PROVIDER,
		Code:      result.ErrorClass,
		Message:   result.Message,
		Retryable: result.Retryable,
		LatencyMs: result.LatencyMS,
	}
	if probeErr != nil {
		providerCheck.Status = rolloutv1.PreflightCheckStatus_PREFLIGHT_CHECK_STATUS_FAIL
		report.Checks = append(report.Checks, providerCheck)
		return preflightResult{UsageReservationID: reservationID}, &preflightFailure{report: report, err: probeErr}
	}
	providerCheck.Status = rolloutv1.PreflightCheckStatus_PREFLIGHT_CHECK_STATUS_PASS
	providerCheck.Code = "ok"
	report.Checks = append(report.Checks, providerCheck)
	pass(rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_BUDGET, "provider probe usage is within rollout budget")
	return preflightResult{Report: report, UsageReservationID: reservationID}, nil
}

func preflightReservationID(work *workerrolloutv1.WorkItem, leaseToken string) string {
	leaseDigest := sha256.Sum256([]byte(leaseToken))
	return "usage-preflight-" + work.GetRolloutID() + fmt.Sprintf("-g%d-c%s", work.GetExecutionGeneration(), hex.EncodeToString(leaseDigest[:8]))
}

func (w Worker) estimateCost(model string, usage *domain.UsageMetrics) *domain.CostMetrics {
	if w.config.EstimateCost != nil {
		return w.config.EstimateCost(model, usage)
	}
	return proxy.EstimateCost(model, usage)
}
