package managedworker

import (
	"context"
	"fmt"

	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	axernbackend "github.com/cofy-x/axern/apps/axrun/internal/backend/axern"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
)

func paramsFromWork(ctx context.Context, work *workerrolloutv1.WorkItem, leaseToken string, config Config) (approllout.Params, error) {
	if config.ExecutionContext == nil {
		return approllout.Params{}, fmt.Errorf("Axern execution context is required")
	}
	spec := work.GetRollout().GetSpec()
	if spec == nil {
		return approllout.Params{}, fmt.Errorf("work has no rollout spec")
	}
	agent := spec.GetAgent()
	selected := []string{}
	attempts := int(spec.GetExecution().GetAttempts())
	execute := work.GetKind() == workerrolloutv1.WorkKind_WORK_KIND_EPISODE
	if execute {
		selected = []string{work.GetEpisode().GetTaskID()}
		attempts = 1
	}
	runID := approllout.ControlRunID(work.GetRolloutID(), work.GetEpisodeID(), work.GetExecutionGeneration())
	if execute {
		runID = ""
	}
	execution := config.ExecutionContext
	params := approllout.Params{
		Context:             ctx,
		TaskSetRef:          spec.GetTaskSetRef(),
		Agent:               agent.GetName(),
		AgentImage:          agent.GetImage(),
		AgentProfile:        agent.GetProfile(),
		AgentApprovalPolicy: agent.GetApprovalPolicy(),
		AgentCommand:        agent.GetCommand(),
		Model:               spec.GetModel(),
		RuntimeClass:        spec.GetExecution().GetRuntimeClass(),
		RunID:               runID,
		SelectedTaskIDs:     selected,
		TaskLimit:           int(spec.GetSelection().GetLimit()),
		ShardIndex:          int(spec.GetSelection().GetShardIndex()),
		ShardCount:          int(spec.GetSelection().GetShardCount()),
		Execute:             execute,
		BackendName:         string(backend.NameAxern),
		Concurrency:         1,
		Attempts:            attempts,
		Output:              config.OutputDir,
		AxernConfig: &axernbackend.Config{
			Endpoint:              execution.Endpoint,
			Namespace:             work.GetRollout().GetNamespace(),
			RuntimeClass:          spec.GetExecution().GetRuntimeClass(),
			TLSCACert:             execution.TLS.CACert,
			TLSCert:               execution.TLS.Cert,
			TLSKey:                execution.TLS.Key,
			TLSServerName:         execution.TLS.ServerName,
			ProxyMode:             execution.ProxyMode,
			RolloutExecutionLease: leaseToken,
		},
	}
	if execute {
		params.TaskLimit = 0
		params.ShardIndex = 0
		params.ShardCount = 0
	}
	return params, nil
}
