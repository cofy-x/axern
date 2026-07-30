package managedworker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestEpisodeProtoPreservesNodeObservedExecutionFacts(t *testing.T) {
	work := &workerrolloutv1.WorkItem{
		RolloutID:           "rol-1",
		EpisodeID:           "eps-1",
		ExecutionGeneration: 2,
		Rollout: &rolloutv1.Rollout{Preflight: &rolloutv1.PreflightReport{
			AgentBundleDigest: "registry/agent@sha256:agent",
		}},
		Episode: &rolloutv1.Episode{TaskID: "task-1", TaskDigest: "sha256:task", AttemptIndex: 1},
	}
	value := approllout.ControlEpisode{Episode: domain.Episode{
		Status: domain.EpisodeStatusCompleted,
		SandboxState: &domain.SandboxRuntimeState{
			AllocationID:          "alloc-1",
			NodeID:                "node-1",
			RuntimeClass:          "runsc",
			PayloadFormat:         "nydus",
			PayloadDigest:         "sha256:payload",
			CacheHit:              true,
			ImageResolveMs:        3,
			ImagePullMs:           5,
			CowPrepareMs:          7,
			VerifierMaterializeMs: 11,
		},
	}}

	facts := episodeProto(work, value).GetExecutionFacts()
	if facts.GetPayloadFormat() != "nydus" || facts.GetPayloadDigest() != "sha256:payload" || !facts.GetCacheHit() ||
		facts.GetImageResolveMs() != 3 || facts.GetImagePullMs() != 5 || facts.GetCowPrepareMs() != 7 ||
		facts.GetVerifierMaterializeMs() != 11 || facts.GetAgentBundleDigest() != "registry/agent@sha256:agent" {
		t.Fatalf("execution facts = %+v", facts)
	}
}

type temporaryWorkerError struct{ temporary bool }

func (e temporaryWorkerError) Error() string   { return "registry unavailable" }
func (e temporaryWorkerError) Temporary() bool { return e.temporary }

func TestPrepareOutputSessionIsolatesExistingContent(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(existing, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := prepareOutputSession(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepareOutputSession(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(filepath.Base(first), workerSessionPrefix) || !strings.HasPrefix(filepath.Base(second), workerSessionPrefix) {
		t.Fatalf("sessions are not isolated: first=%q second=%q", first, second)
	}
	if _, err := os.Stat(existing); err != nil {
		t.Fatalf("existing output-root content was changed: %v", err)
	}
}

func TestParamsFromWorkUsesDedicatedExecutionContext(t *testing.T) {
	work := &workerrolloutv1.WorkItem{
		Kind: workerrolloutv1.WorkKind_WORK_KIND_EPISODE,
		Rollout: &rolloutv1.Rollout{
			Namespace: "default",
			Spec: &rolloutv1.RolloutSpec{
				Agent:     &rolloutv1.RolloutAgent{Name: "command"},
				Execution: &rolloutv1.RolloutExecution{Attempts: 1},
				Selection: &rolloutv1.TaskSetSelection{},
			},
		},
		Episode: &rolloutv1.Episode{TaskID: "task-1"},
	}
	params, err := paramsFromWork(context.Background(), work, "lease-test", Config{
		ControlContext:   &clientconfig.Context{Endpoint: "controld:24000"},
		ExecutionContext: &clientconfig.Context{Endpoint: "gatewayd:25000", TLS: clientconfig.TLS{ServerName: "gatewayd"}},
		OutputDir:        t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if params.AxernConfig.Endpoint != "gatewayd:25000" || params.AxernConfig.TLSServerName != "gatewayd" || params.AxernConfig.RolloutExecutionLease != "lease-test" {
		t.Fatalf("execution config = %+v", params.AxernConfig)
	}
}

func TestParamsFromWorkRequiresExecutionContext(t *testing.T) {
	_, err := paramsFromWork(context.Background(), &workerrolloutv1.WorkItem{}, "", Config{})
	if err == nil || !strings.Contains(err.Error(), "execution context is required") {
		t.Fatalf("paramsFromWork() error = %v", err)
	}
}

func TestReservationForExcludesCommittedPreflightUsage(t *testing.T) {
	rollout := &rolloutv1.Rollout{
		Spec:    &rolloutv1.RolloutSpec{Budget: &rolloutv1.RolloutBudget{MaxTokens: 1024, MaxCostMicrousd: 10_000}},
		Summary: &rolloutv1.RolloutSummary{EpisodeCount: 2},
		Preflight: &rolloutv1.PreflightReport{Usage: &rolloutv1.PreflightUsage{
			InputTokens: 7, OutputTokens: 1, CostMicrousd: 9,
		}},
	}

	tokens, cost := reservationFor(rollout)
	if tokens != 508 || cost != 4_995 {
		t.Fatalf("reservationFor() = (%d, %d), want (508, 4995)", tokens, cost)
	}
}

func TestZeroEpisodeAllowanceRemainsZero(t *testing.T) {
	rollout := &rolloutv1.Rollout{
		Spec:      &rolloutv1.RolloutSpec{Budget: &rolloutv1.RolloutBudget{MaxTokens: 1}},
		Summary:   &rolloutv1.RolloutSummary{EpisodeCount: 2},
		Preflight: &rolloutv1.PreflightReport{Usage: &rolloutv1.PreflightUsage{InputTokens: 1}},
	}
	tokens, cost := reservationFor(rollout)
	if tokens != 0 || cost != 0 {
		t.Fatalf("reservationFor() = (%d, %d), want zero allowance", tokens, cost)
	}
}

func TestPreflightReservationIsUniquePerWorkLease(t *testing.T) {
	work := &workerrolloutv1.WorkItem{RolloutID: "rol-test", ExecutionGeneration: 3}
	first := preflightReservationID(work, "lease-one")
	second := preflightReservationID(work, "lease-two")
	if first == second {
		t.Fatalf("preflight reservation reused across leases: %s", first)
	}
	if first != preflightReservationID(work, "lease-one") {
		t.Fatal("preflight reservation is not stable within one lease")
	}
}

func TestIsRetriableUsesTemporaryInfrastructureContract(t *testing.T) {
	if !isRetriable(fmt.Errorf("resolve TaskSet: %w", temporaryWorkerError{temporary: true})) {
		t.Fatal("temporary registry error was not retryable")
	}
	if isRetriable(errors.New("descriptor contract mismatch")) {
		t.Fatal("permanent descriptor error was retryable")
	}
	if isRetriable(status.Error(codes.ResourceExhausted, "rollout usage budget is exhausted")) {
		t.Fatal("durable budget exhaustion was retryable")
	}
}
