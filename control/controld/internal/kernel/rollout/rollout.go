package rollout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"google.golang.org/protobuf/proto"
)

const (
	DefaultPageSize            = 50
	MaxPageSize                = 200
	DefaultWorkLease           = 30 * time.Second
	DefaultWorkerSession       = 5 * time.Minute
	DefaultInfrastructureRetry = 2
	MaxEpisodesPerRollout      = 100000
	MaxAttemptsPerTask         = 100
	MaxRolloutConcurrency      = 10000
)

type CreateParams struct {
	Namespace      string
	Spec           *rolloutv1.RolloutSpec
	Labels         map[string]string
	IdempotencyKey string
	StartPolicy    rolloutv1.RolloutStartPolicy
}

type PlanResult struct {
	ResultDigest     string
	SourceDigest     string
	DescriptorDigest string
	PlanJSON         []byte
	Tasks            []*workerrolloutv1.PlannedTask
}

type Store interface {
	Create(ctx context.Context, params CreateParams, now time.Time) (*rolloutv1.Rollout, error)
	Start(ctx context.Context, id, idempotencyKey string, now time.Time) (*rolloutv1.Rollout, bool, error)
	Get(ctx context.Context, id string) (*rolloutv1.Rollout, []*rolloutv1.Episode, bool, error)
	List(ctx context.Context, filter *rolloutv1.RolloutListFilter) ([]*rolloutv1.Rollout, string, error)
	Cancel(ctx context.Context, id string, now time.Time) (*rolloutv1.Rollout, bool, error)
	Retry(ctx context.Context, id string, now time.Time) (*rolloutv1.Rollout, bool, error)
	Delete(ctx context.Context, id string, now time.Time) (*rolloutv1.Rollout, bool, error)
	ListEvents(ctx context.Context, rolloutID string, afterSequence int64, limit int) ([]*rolloutv1.RolloutEvent, int64, bool, error)
	Compare(ctx context.Context, rolloutIDs []string) ([]*rolloutv1.Rollout, []*rolloutv1.TaskComparison, error)
}

type EventWaiter interface {
	WaitForEvents(ctx context.Context, rolloutID string, afterSequence int64, timeout time.Duration) error
}

type WorkerStore interface {
	RegisterWorker(ctx context.Context, req *workerrolloutv1.RegisterWorkerRequest, tokenHash string, now time.Time, ttl time.Duration) (*workerrolloutv1.RegisterWorkerResponse, error)
	ClaimWork(ctx context.Context, sessionID, sessionTokenHash string, now time.Time, ttl time.Duration) (*workerrolloutv1.WorkItem, string, error)
	RenewWork(ctx context.Context, workID, leaseTokenHash string, now time.Time, ttl time.Duration) (time.Time, bool, error)
	ReportProgress(ctx context.Context, req *workerrolloutv1.ReportWorkProgressRequest, leaseTokenHash string, now time.Time) (bool, error)
	CompletePlan(ctx context.Context, req *workerrolloutv1.CompletePlanRequest, leaseTokenHash string, now time.Time) (*rolloutv1.Rollout, error)
	CompleteEpisode(ctx context.Context, req *workerrolloutv1.CompleteEpisodeRequest, leaseTokenHash string, now time.Time) (*rolloutv1.Rollout, error)
	CompleteProfileDoctor(ctx context.Context, req *workerrolloutv1.CompleteProfileDoctorRequest, leaseTokenHash string, now time.Time) error
	FailWork(ctx context.Context, req *workerrolloutv1.FailWorkRequest, leaseTokenHash string, now time.Time) (*rolloutv1.Rollout, error)
	ResolveAgentProfile(ctx context.Context, req *workerrolloutv1.ResolveAgentProfileRequest, leaseTokenHash string, now time.Time) (*workerrolloutv1.ResolvedAgentProfile, error)
	ReserveUsage(ctx context.Context, req *workerrolloutv1.ReserveUsageRequest, leaseTokenHash string, now time.Time) (int64, int64, error)
	CommitUsage(ctx context.Context, req *workerrolloutv1.CommitUsageRequest, leaseTokenHash string, now time.Time) (int64, int64, error)
	ReleaseUsage(ctx context.Context, req *workerrolloutv1.ReleaseUsageRequest, leaseTokenHash string, now time.Time) error
	WaitForWork(ctx context.Context, sessionID, sessionTokenHash string, timeout time.Duration) error
	CreateArtifactUpload(ctx context.Context, req *workerrolloutv1.CreateArtifactUploadRequest, leaseTokenHash string, now time.Time, ttl time.Duration) (*rolloutv1.Artifact, ArtifactUpload, error)
	CommitArtifact(ctx context.Context, req *workerrolloutv1.CommitArtifactRequest, leaseTokenHash string, now time.Time) (*rolloutv1.Artifact, error)
}

func ValidateCreate(params CreateParams) error {
	if strings.TrimSpace(params.Namespace) == "" {
		return fmt.Errorf("namespace is required")
	}
	if params.Spec == nil {
		return fmt.Errorf("spec is required")
	}
	if ref := strings.TrimSpace(params.Spec.GetTaskSetRef()); !immutableReference(ref) {
		return fmt.Errorf("task_set_ref must use an immutable repository@sha256 digest")
	}
	if params.Spec.GetAgent() == nil || strings.TrimSpace(params.Spec.GetAgent().GetName()) == "" {
		return fmt.Errorf("agent.name is required")
	}
	agent := params.Spec.GetAgent()
	if agent.GetName() == "command" {
		if strings.TrimSpace(agent.GetCommand()) == "" {
			return fmt.Errorf("command agent requires agent.command")
		}
		if agent.GetProfile() != "" || agent.GetImage() != "" || params.Spec.GetModel() != "" || agent.GetApprovalPolicy() != "" {
			return fmt.Errorf("command agent does not accept profile, image, model, or approval_policy")
		}
	} else {
		if strings.TrimSpace(agent.GetProfile()) == "" || strings.TrimSpace(params.Spec.GetModel()) == "" {
			return fmt.Errorf("managed agent requires agent.profile and model")
		}
		if agent.GetCommand() != "" {
			return fmt.Errorf("managed agent does not accept agent.command")
		}
		if agent.GetApprovalPolicy() != "never" {
			return fmt.Errorf("managed Axern rollout requires approval_policy never")
		}
		if agent.GetImage() != "" && !immutableReference(agent.GetImage()) {
			return fmt.Errorf("agent.image must use an immutable repository@sha256 digest")
		}
	}
	if params.StartPolicy != rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_MANUAL && params.StartPolicy != rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO {
		return fmt.Errorf("start_policy must be manual or auto")
	}
	if params.Spec.GetExecution() == nil || params.Spec.GetExecution().GetConcurrency() <= 0 || params.Spec.GetExecution().GetAttempts() <= 0 {
		return fmt.Errorf("execution.concurrency and execution.attempts must be greater than zero")
	}
	if params.Spec.GetExecution().GetConcurrency() > MaxRolloutConcurrency || params.Spec.GetExecution().GetAttempts() > MaxAttemptsPerTask {
		return fmt.Errorf("execution exceeds supported concurrency or attempts limit")
	}
	selection := params.Spec.GetSelection()
	if selection.GetLimit() < 0 || selection.GetShardIndex() < 0 || selection.GetShardCount() < 0 || (selection.GetShardCount() == 0 && selection.GetShardIndex() != 0) || (selection.GetShardCount() > 0 && selection.GetShardIndex() >= selection.GetShardCount()) {
		return fmt.Errorf("selection limit or shard is invalid")
	}
	if budget := params.Spec.GetBudget(); budget != nil {
		if budget.GetMaxWallTime() != nil && budget.GetMaxWallTime().AsDuration() <= 0 {
			return fmt.Errorf("budget.max_wall_time must be greater than zero")
		}
		if budget.GetMaxEpisodes() < 0 || budget.GetMaxTokens() < 0 || budget.GetMaxCostMicrousd() < 0 {
			return fmt.Errorf("budget values must not be negative")
		}
		if budget.GetMaxEpisodes() > MaxEpisodesPerRollout {
			return fmt.Errorf("budget.max_episodes exceeds supported limit")
		}
		if (budget.GetMaxTokens() > 0 || budget.GetMaxCostMicrousd() > 0) && params.Spec.GetAgent().GetName() == "command" {
			return fmt.Errorf("token and cost budgets require a managed metered agent")
		}
	}
	return nil
}

func SpecHash(namespace string, spec *rolloutv1.RolloutSpec, labels map[string]string, startPolicy rolloutv1.RolloutStartPolicy) (string, error) {
	payload := &createHashEnvelope{Namespace: strings.TrimSpace(namespace), Spec: spec, Labels: labels, StartPolicy: startPolicy}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload.asProto())
	if err != nil {
		return "", fmt.Errorf("marshal rollout request: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// createHashEnvelope uses a protobuf carrier so map ordering is deterministic.
type createHashEnvelope struct {
	Namespace   string
	Spec        *rolloutv1.RolloutSpec
	Labels      map[string]string
	StartPolicy rolloutv1.RolloutStartPolicy
}

func (e *createHashEnvelope) asProto() *rolloutv1.CreateRolloutRequest {
	return &rolloutv1.CreateRolloutRequest{Namespace: e.Namespace, Spec: e.Spec, Labels: e.Labels, StartPolicy: e.StartPolicy}
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func IsTerminal(status rolloutv1.RolloutStatus) bool {
	return status == rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED ||
		status == rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED ||
		status == rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLED ||
		status == rolloutv1.RolloutStatus_ROLLOUT_STATUS_DELETING
}

func immutableReference(ref string) bool {
	parts := strings.Split(ref, "@sha256:")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || len(parts[1]) != 64 {
		return false
	}
	_, err := hex.DecodeString(parts[1])
	return err == nil
}
