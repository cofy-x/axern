package publicv1

import (
	"context"
	"fmt"
	"strings"
	"time"

	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) CreateRollout(ctx context.Context, req *rolloutv1.CreateRolloutRequest) (*rolloutv1.CreateRolloutResponse, error) {
	rollout, err := s.deps.Rollouts.Create(ctx, rolloutkernel.CreateParams{Namespace: req.GetNamespace(), Spec: req.GetSpec(), Labels: req.GetLabels(), IdempotencyKey: req.GetIdempotencyKey(), StartPolicy: req.GetStartPolicy()}, s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &rolloutv1.CreateRolloutResponse{Rollout: rollout}, nil
}
func (s *Server) StartRollout(ctx context.Context, req *rolloutv1.StartRolloutRequest) (*rolloutv1.StartRolloutResponse, error) {
	rollout, ok, err := s.deps.Rollouts.Start(ctx, req.GetRolloutID(), req.GetIdempotencyKey(), s.deps.Now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "rollout not found")
	}
	return &rolloutv1.StartRolloutResponse{Rollout: rollout}, nil
}
func (s *Server) GetRollout(ctx context.Context, req *rolloutv1.GetRolloutRequest) (*rolloutv1.GetRolloutResponse, error) {
	rollout, episodes, ok, err := s.deps.Rollouts.Get(ctx, strings.TrimSpace(req.GetRolloutID()))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "rollout not found")
	}
	return &rolloutv1.GetRolloutResponse{Rollout: rollout, Episodes: episodes}, nil
}
func (s *Server) ListRollouts(ctx context.Context, req *rolloutv1.ListRolloutsRequest) (*rolloutv1.ListRolloutsResponse, error) {
	rollouts, next, err := s.deps.Rollouts.List(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	return &rolloutv1.ListRolloutsResponse{Rollouts: rollouts, NextCursor: next}, nil
}
func (s *Server) CancelRollout(ctx context.Context, req *rolloutv1.CancelRolloutRequest) (*rolloutv1.CancelRolloutResponse, error) {
	rollout, ok, err := s.deps.Rollouts.Cancel(ctx, req.GetRolloutID(), s.deps.Now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "rollout not found")
	}
	return &rolloutv1.CancelRolloutResponse{Rollout: rollout}, nil
}
func (s *Server) RetryRollout(ctx context.Context, req *rolloutv1.RetryRolloutRequest) (*rolloutv1.RetryRolloutResponse, error) {
	rollout, ok, err := s.deps.Rollouts.Retry(ctx, req.GetRolloutID(), s.deps.Now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "rollout not found")
	}
	return &rolloutv1.RetryRolloutResponse{Rollout: rollout}, nil
}
func (s *Server) DeleteRollout(ctx context.Context, req *rolloutv1.DeleteRolloutRequest) (*rolloutv1.DeleteRolloutResponse, error) {
	rollout, ok, err := s.deps.Rollouts.Delete(ctx, req.GetRolloutID(), s.deps.Now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "rollout not found")
	}
	return &rolloutv1.DeleteRolloutResponse{Rollout: rollout}, nil
}
func (s *Server) CompareRollouts(ctx context.Context, req *rolloutv1.CompareRolloutsRequest) (*rolloutv1.CompareRolloutsResponse, error) {
	rollouts, tasks, err := s.deps.Rollouts.Compare(ctx, req.GetRolloutIds())
	if err != nil {
		return nil, err
	}
	return &rolloutv1.CompareRolloutsResponse{Rollouts: rollouts, Tasks: tasks}, nil
}
func (s *Server) ListArtifacts(ctx context.Context, req *rolloutv1.ListArtifactsRequest) (*rolloutv1.ListArtifactsResponse, error) {
	reader, ok := s.deps.Rollouts.(rolloutkernel.ArtifactReader)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "rollout artifact store is not configured")
	}
	artifacts, err := reader.ListArtifacts(ctx, req.GetRolloutID(), req.GetEpisodeID())
	if err != nil {
		return nil, err
	}
	return &rolloutv1.ListArtifactsResponse{Artifacts: artifacts}, nil
}
func (s *Server) PrepareArtifactDownload(ctx context.Context, req *rolloutv1.PrepareArtifactDownloadRequest) (*rolloutv1.PrepareArtifactDownloadResponse, error) {
	result := "error"
	defer func() {
		sdkobs.Int64Counter(ctrlobs.MetricArtifactTicketTotal.Name, ctrlobs.MetricArtifactTicketTotal.Description).Add(ctx, 1,
			attribute.String(sdkobs.AttrOperation, "issue"), attribute.String(sdkobs.AttrResult, result))
	}()
	reader, ok := s.deps.Rollouts.(rolloutkernel.ArtifactReader)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "rollout artifact store is not configured")
	}
	artifact, ticket, expires, found, err := reader.PrepareArtifactDownload(ctx, req.GetArtifactID(), 15*time.Minute)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, status.Error(codes.NotFound, "artifact not found")
	}
	result = "ok"
	return &rolloutv1.PrepareArtifactDownloadResponse{Artifact: artifact, Ticket: ticket, ExpiresAt: timestamppb.New(expires)}, nil
}
func (s *Server) DiagnoseRollout(ctx context.Context, req *rolloutv1.DiagnoseRolloutRequest) (*rolloutv1.DiagnoseRolloutResponse, error) {
	rollout, episodes, ok, err := s.deps.Rollouts.Get(ctx, req.GetRolloutID())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "rollout not found")
	}
	response := &rolloutv1.DiagnoseRolloutResponse{Rollout: rollout, Diagnosis: rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_HEALTHY, Code: "healthy", Summary: "rollout state is healthy", RecommendedAction: "no action required"}
	if reader, ok := s.deps.Rollouts.(rolloutkernel.ArtifactReader); ok {
		response.Artifacts, err = reader.ListArtifacts(ctx, rollout.GetID(), "")
		if err != nil {
			return nil, err
		}
	}
	classifyDiagnosis(response, episodes)
	return response, nil
}
func classifyDiagnosis(response *rolloutv1.DiagnoseRolloutResponse, episodes []*rolloutv1.Episode) {
	rollout := response.GetRollout()
	set := func(class rolloutv1.DiagnosisClass, code, summary, action string) {
		response.Diagnosis = class
		response.Code = code
		response.Summary = summary
		response.RecommendedAction = action
	}
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLING {
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_CANCEL_PENDING, "cancel_pending", "worker cancellation has not converged", "wait for the active worker lease to expire")
		return
	}
	for _, check := range rollout.GetPreflight().GetChecks() {
		if check.GetStatus() == rolloutv1.PreflightCheckStatus_PREFLIGHT_CHECK_STATUS_FAIL {
			class := rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_PLANNING_REJECTED
			if check.GetKind() == rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_PROVIDER || check.GetKind() == rolloutv1.PreflightCheckKind_PREFLIGHT_CHECK_KIND_PROFILE {
				class = rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_PROFILE_PROVIDER_FAILURE
			}
			set(class, check.GetCode(), check.GetMessage(), "fix the reported preflight condition and create a new rollout")
			return
		}
	}
	switch rollout.GetFailureClass() {
	case rolloutv1.FailureClass_FAILURE_CLASS_BUDGET, rolloutv1.FailureClass_FAILURE_CLASS_METERING:
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_BUDGET_EXHAUSTION, "budget_exhausted", rollout.GetMessage(), "create a new rollout with a sufficient validated budget")
		return
	case rolloutv1.FailureClass_FAILURE_CLASS_INFRASTRUCTURE:
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_INFRASTRUCTURE_FAILURE, "infrastructure_failure", rollout.GetMessage(), "retry the rollout after resolving infrastructure health")
		return
	case rolloutv1.FailureClass_FAILURE_CLASS_AGENT, rolloutv1.FailureClass_FAILURE_CLASS_VERIFIER:
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_TASK_VERIFIER_FAILURE, "task_failure", rollout.GetMessage(), "inspect episode evidence and task/verifier output")
		return
	}
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_ACCEPTED || rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_PLANNING {
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_WORKER_UNAVAILABLE, "planning_wait", "planning work is waiting for a capable worker", "check rollout worker sessions and capabilities")
		return
	}
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_QUEUED {
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_QUEUE_WAITING, "queue_wait", "episodes are queued", "check worker capacity and profile concurrency")
		return
	}
	var budgetFailure, infrastructureFailure, taskFailure *rolloutv1.Episode
	for _, episode := range episodes {
		switch episode.GetFailureClass() {
		case rolloutv1.FailureClass_FAILURE_CLASS_BUDGET, rolloutv1.FailureClass_FAILURE_CLASS_METERING:
			if budgetFailure == nil {
				budgetFailure = episode
			}
		case rolloutv1.FailureClass_FAILURE_CLASS_INFRASTRUCTURE:
			if infrastructureFailure == nil {
				infrastructureFailure = episode
			}
		case rolloutv1.FailureClass_FAILURE_CLASS_AGENT, rolloutv1.FailureClass_FAILURE_CLASS_VERIFIER:
			if taskFailure == nil {
				taskFailure = episode
			}
		}
	}
	if budgetFailure != nil {
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_BUDGET_EXHAUSTION, "budget_exhausted", budgetFailure.GetMessage(), "create a new rollout with a sufficient validated budget")
		return
	}
	if infrastructureFailure != nil {
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_INFRASTRUCTURE_FAILURE, "infrastructure_failure", infrastructureFailure.GetMessage(), "retry the rollout after resolving infrastructure health")
		return
	}
	if taskFailure != nil {
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_TASK_VERIFIER_FAILURE, "task_failure", taskFailure.GetMessage(), "inspect episode evidence and task/verifier output")
		return
	}
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_RUNNING {
		for _, episode := range episodes {
			if episode.GetStatus() == rolloutv1.EpisodeStatus_EPISODE_STATUS_PENDING {
				set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_CAPACITY_WAIT, "capacity_wait", "some episodes are waiting behind active rollout or Profile concurrency", "inspect worker capacity and the frozen Profile concurrency limit")
				return
			}
		}
	}
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED {
		set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_INFRASTRUCTURE_FAILURE, "infrastructure_failure", rollout.GetMessage(), "inspect planning or worker infrastructure and create a new rollout after recovery")
		return
	}
	present := make(map[string]map[string]struct{}, len(episodes))
	for _, artifact := range response.GetArtifacts() {
		if artifact.GetStatus() == rolloutv1.ArtifactStatus_ARTIFACT_STATUS_PRESENT {
			key := fmt.Sprintf("%s/%d", artifact.GetEpisodeID(), artifact.GetExecutionGeneration())
			if present[key] == nil {
				present[key] = map[string]struct{}{}
			}
			present[key][artifact.GetKind()] = struct{}{}
		}
	}
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED {
		required := [...]string{"task", "episode", "trajectory", "agent", "verifier", "reward", "manifest"}
		for _, episode := range episodes {
			key := fmt.Sprintf("%s/%d", episode.GetID(), episode.GetExecutionGeneration())
			for _, kind := range required {
				if _, ok := present[key][kind]; !ok {
					set(rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_INCOMPLETE_EVIDENCE, "incomplete_evidence", "one or more episodes lack the complete evidence set", "inspect artifact inventory and worker artifact upload health")
					return
				}
			}
		}
	}
}
func (s *Server) WatchRolloutEvents(req *rolloutv1.WatchRolloutEventsRequest, stream rolloutv1.RolloutControl_WatchRolloutEventsServer) error {
	after := req.GetAfterSequence()
	for {
		events, current, terminal, err := s.deps.Rollouts.ListEvents(stream.Context(), req.GetRolloutID(), after, rolloutkernel.MaxPageSize)
		if err != nil {
			return err
		}
		if len(events) > 0 || terminal {
			if err := stream.Send(&rolloutv1.WatchRolloutEventsResponse{Events: events, CurrentSequence: current, Terminal: terminal}); err != nil {
				return err
			}
			after = current
		}
		if terminal {
			return nil
		}
		if waiter, ok := s.deps.Rollouts.(rolloutkernel.EventWaiter); ok {
			if err := waiter.WaitForEvents(stream.Context(), req.GetRolloutID(), after, 30*time.Second); err != nil {
				return err
			}
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-stream.Context().Done():
			timer.Stop()
			return stream.Context().Err()
		case <-timer.C:
		}
	}
}
