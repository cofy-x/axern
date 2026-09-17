package apprun

import (
	"context"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Control interface {
	CreateRun(ctx context.Context, params runkernel.CreateParams, now time.Time) (*runv1.Run, error)
	GetRun(ctx context.Context, id string) (*runv1.Run, error)
	WatchRun(ctx context.Context, id string, afterVersion int64) (*runv1.Run, error)
	ListRuns(ctx context.Context, filter *runv1.RunListFilter) ([]*runv1.Run, string, error)
	CancelRun(ctx context.Context, runID string, now time.Time) (*runv1.Run, error)
}

type CandidateSelector interface {
	SelectCandidates(ctx context.Context, env *environmentv1.Environment, config *commonv1.ExecutionConfig) ([]*placementkernel.Candidate, error)
}

type AllocationLifecycle interface {
	CreateAllocation(ctx context.Context, target string, run *runv1.Run, env *environmentv1.Environment, nodeID string, requirements []*capabilityv1.CapabilityRequirement) (*capabilityv1.CapabilityConditionSet, error)
	DeleteAllocation(ctx context.Context, target, allocationID string, nodeID string, outputSealing *allocationkernel.OutputSealing) error
}

type AuthoritativeStore interface {
	runkernel.EnvironmentStore
	runkernel.RunStore
}

func NewAuthoritative(store AuthoritativeStore, selector CandidateSelector) Control {
	return authoritativeRunAccess{store: store, selector: selector}
}

type authoritativeRunAccess struct {
	store    AuthoritativeStore
	selector CandidateSelector
}

func (p authoritativeRunAccess) CreateRun(ctx context.Context, params runkernel.CreateParams, now time.Time) (*runv1.Run, error) {
	environmentID := strings.TrimSpace(params.EnvironmentID)
	if environmentID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "environment_id is required")
	}
	env, err := p.store.GetEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	namespace := environmentkernel.NormalizeNamespace(params.Namespace)
	if namespace != env.GetNamespace() {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "run namespace %q does not own environment %q", namespace, environmentID)
	}
	normalizedConfig, err := executionkernel.NormalizeConfigForRootfs(params.Config, env.GetResolvedSpec().GetRootfsReadonly())
	if err != nil {
		return nil, err
	}
	candidates, err := p.selector.SelectCandidates(ctx, env, normalizedConfig)
	if err != nil {
		return nil, err
	}
	run, err := p.store.AdmitRun(ctx, runkernel.AdmitRunParams{
		Namespace:   namespace,
		Environment: env,
		Config:      normalizedConfig,
		Labels:      params.Labels,
		Candidates:  candidates,
	}, now)
	if err != nil {
		return nil, err
	}
	return run, nil
}

func (p authoritativeRunAccess) GetRun(ctx context.Context, id string) (*runv1.Run, error) {
	return p.store.GetRun(ctx, id)
}

func (p authoritativeRunAccess) WatchRun(ctx context.Context, id string, afterVersion int64) (*runv1.Run, error) {
	return p.store.WatchRun(ctx, id, afterVersion)
}

func (p authoritativeRunAccess) ListRuns(ctx context.Context, filter *runv1.RunListFilter) ([]*runv1.Run, string, error) {
	return p.store.ListRuns(ctx, filter)
}

func (p authoritativeRunAccess) CancelRun(ctx context.Context, runID string, now time.Time) (*runv1.Run, error) {
	return p.store.CancelRun(ctx, runID, now)
}
