package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appcatalog "github.com/cofy-x/axern/apps/cli/internal/application/catalog"
	appenvironment "github.com/cofy-x/axern/apps/cli/internal/application/environment"
	"github.com/cofy-x/axern/lib/go/agentprofile"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type DoctorParams struct {
	Profile        agentprofile.Profile
	Workspace      string
	ProbeModel     string
	ProviderProber ProviderProber
	ServiceClient  ServiceClient
	Catalog        appcatalog.RuntimeCatalogClient
	Environment    appenvironment.EnvironmentClient
}

type DoctorResult struct {
	Agent               string                    `json:"agent"`
	Provider            string                    `json:"provider"`
	Profile             string                    `json:"profile"`
	Workspace           string                    `json:"workspace"`
	WorkspaceTemplate   string                    `json:"workspace_template"`
	AgentBundle         string                    `json:"agent_bundle"`
	ConfigOK            bool                      `json:"config_ok"`
	ApprovalCompatible  bool                      `json:"approval_compatible"`
	AxernApprovalPolicy string                    `json:"axern_approval_policy"`
	LocalApprovalPolicy string                    `json:"local_approval_policy"`
	ServiceID           string                    `json:"service_id,omitempty"`
	ReadyReplicas       int32                     `json:"ready_replicas,omitempty"`
	DesiredReplicas     int32                     `json:"desired_replicas,omitempty"`
	LifecycleState      string                    `json:"lifecycle_state,omitempty"`
	Persistent          bool                      `json:"persistent"`
	Recommendation      string                    `json:"recommendation"`
	UpstreamCheck       *agentprofile.ProbeResult `json:"upstream_check,omitempty"`
	PlatformCheck       *DoctorPlatformCheck      `json:"platform_check,omitempty"`
}

type DoctorPlatformCheck struct {
	Reachable  bool   `json:"reachable"`
	ErrorClass string `json:"error_class,omitempty"`
	Message    string `json:"message"`
}

func (Control) Doctor(ctx context.Context, params DoctorParams) (DoctorResult, error) {
	if err := ValidateProfile(params.Profile); err != nil {
		return DoctorResult{}, err
	}
	workspace, err := ResolveWorkspaceName(params.Workspace, params.Profile.Name)
	if err != nil {
		return DoctorResult{}, err
	}
	adapter, err := AdapterFor(params.Profile.Agent)
	if err != nil {
		return DoctorResult{}, err
	}
	result := DoctorResult{
		Agent:               string(params.Profile.Agent),
		Provider:            string(params.Profile.ProviderType),
		Profile:             normalizedProfileName(params.Profile.Name),
		Workspace:           workspace,
		WorkspaceTemplate:   firstNonEmpty(params.Profile.TemplateID, adapter.DefaultTemplateID()),
		AgentBundle:         adapter.BundleID(),
		ConfigOK:            true,
		ApprovalCompatible:  true,
		AxernApprovalPolicy: "never",
		LocalApprovalPolicy: "on_request",
	}
	upstreamCheck, probeErr := probeProvider(ctx, params.Profile, params.ProbeModel, params.ProviderProber)
	result.UpstreamCheck = &upstreamCheck
	if probeErr != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if upstreamCheck.ErrorClass == agentprofile.ProbeErrorInvalidConfig {
			result.ConfigOK = false
		}
	}
	services, err := listAgentServices(ctx, params.ServiceClient, map[string]string{
		LabelWorkflow:  "agent",
		LabelWorkspace: workspace,
	})
	if err != nil {
		return completeDoctorPlatformFailure(ctx, result, probeErr, "list agent services", err)
	}
	services = nonTerminalWorkspaceServices(services)
	if len(services) > 1 {
		return completeDoctorPlatformFailure(ctx, result, probeErr, "inspect agent workspace", fmt.Errorf("multiple active services"))
	}
	if len(services) == 1 && serviceDeletionInProgress(services[0]) {
		return completeDoctorPlatformFailure(ctx, result, probeErr, "inspect agent workspace", fmt.Errorf("agent workspace %q is deleting", workspace))
	}
	runtimeProfile := params.Profile
	if len(services) == 1 {
		runtimeProfile.Namespace = firstNonEmpty(services[0].GetNamespace(), DefaultNamespace)
	}
	bundle, err := resolveAgentBundle(ctx, params.Catalog, adapter)
	if err != nil {
		return completeDoctorPlatformFailure(ctx, result, probeErr, "resolve agent bundle", err)
	}
	environmentIDs, err := currentEnvironmentIDs(ctx, runtimeProfile, params.Catalog, params.Environment)
	if err != nil {
		return completeDoctorPlatformFailure(ctx, result, probeErr, "resolve runtime environment", err)
	}
	if len(services) == 1 {
		service := services[0]
		result.ServiceID = service.GetID()
		result.ReadyReplicas = service.GetReadyReplicas()
		result.DesiredReplicas = service.GetReplicas()
		result.LifecycleState = workspaceLifecycleState(service)
		result.Persistent = workspaceConfigMatches(service, bundle)
	}
	platformRecommendation := ""
	switch {
	case result.ServiceID == "":
		platformRecommendation = "run axern agent shell or axern agent run to create this workspace"
	case result.LifecycleState == LifecycleDegraded:
		platformRecommendation = "inspect workspace service events before reconnecting"
	case services[0].GetLabels()[LabelProfile] != result.Profile:
		platformRecommendation = fmt.Sprintf("stop workspace before switching from profile %q to %q", services[0].GetLabels()[LabelProfile], result.Profile)
	case !result.Persistent:
		platformRecommendation = "workspace volume or read-only agent bundle image mount does not match the catalog; stop the workspace before repairing it"
	case !containsString(environmentIDs, services[0].GetEnvironmentID()):
		platformRecommendation = "workspace base template does not match the catalog; stop the workspace before updating it"
	case result.LifecycleState == LifecycleSuspended:
		platformRecommendation = "workspace is suspended and will resume on the next shell, run, or connect"
	default:
		platformRecommendation = "agent workspace is ready"
	}
	result.PlatformCheck = &DoctorPlatformCheck{Reachable: true, Message: "Axern platform API is reachable"}
	result.Recommendation = joinRecommendations(probeErr, platformRecommendation)
	return result, nil
}

func completeDoctorPlatformFailure(ctx context.Context, result DoctorResult, probeErr error, operation string, err error) (DoctorResult, error) {
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	code := strings.ToLower(status.Code(err).String())
	if code == "unknown" {
		code = "platform_error"
	}
	message := fmt.Sprintf("%s failed (%s): %v", operation, code, err)
	result.PlatformCheck = &DoctorPlatformCheck{ErrorClass: code, Message: message}
	result.Recommendation = joinRecommendations(probeErr, message)
	return result, nil
}

func probeProvider(ctx context.Context, profile agentprofile.Profile, model string, prober ProviderProber) (agentprofile.ProbeResult, error) {
	if prober == nil {
		prober = agentprofile.Prober{}
	}
	if strings.TrimSpace(model) == "" {
		model = agentprofile.ProbeModel(profile)
	}
	return prober.Probe(ctx, agentprofile.ProbeRequest{Profile: profile, Model: model})
}

func joinRecommendations(probeErr error, platform string) string {
	if probeErr == nil {
		return platform
	}
	if strings.TrimSpace(platform) == "" {
		return probeErr.Error()
	}
	return probeErr.Error() + "; " + platform
}

func currentEnvironmentIDs(
	ctx context.Context,
	profile agentprofile.Profile,
	catalog appcatalog.RuntimeCatalogClient,
	environment appenvironment.EnvironmentClient,
) ([]string, error) {
	if catalog == nil {
		return nil, fmt.Errorf("runtime catalog client is required")
	}
	if environment == nil {
		return nil, errEnvironmentResolverRequired()
	}
	adapter, err := AdapterFor(profile.Agent)
	if err != nil {
		return nil, err
	}
	namespace := firstNonEmpty(profile.Namespace, DefaultNamespace)
	templateID := firstNonEmpty(profile.TemplateID, adapter.DefaultTemplateID())
	templateResp, err := catalog.GetRuntimeTemplate(ctx, &catalogv1.GetRuntimeTemplateRequest{ID: templateID})
	if err != nil {
		return nil, err
	}
	template := templateResp.GetRuntimeTemplate()
	if template == nil {
		return nil, fmt.Errorf("runtime template %q was not returned", templateID)
	}
	cursor := ""
	seen := map[string]struct{}{}
	candidates := make([]*environmentv1.Environment, 0)
	for {
		envResp, err := environment.ListEnvironments(ctx, &environmentv1.ListEnvironmentsRequest{Filter: &environmentv1.ListFilter{
			Namespace: namespace,
			Statuses:  []environmentv1.EnvironmentStatus{environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY},
			Cursor:    cursor,
			PageSize:  agentListPageSize,
		}})
		if err != nil {
			return nil, err
		}
		for _, env := range envResp.GetEnvironments() {
			if env == nil || env.GetID() == "" || env.GetStatus() != environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY {
				continue
			}
			spec := env.GetSpec()
			if spec.GetNamespace() == namespace && spec.GetTemplateID() == templateID && spec.GetTemplateVersion() == template.GetVersion() && proto.Equal(env.GetResolvedTemplate(), template) {
				candidates = append(candidates, env)
			}
		}
		next := strings.TrimSpace(envResp.GetNextCursor())
		if next == "" {
			break
		}
		if next == cursor {
			return nil, fmt.Errorf("environment list returned repeated cursor %q", next)
		}
		if _, ok := seen[next]; ok {
			return nil, fmt.Errorf("environment list returned cursor cycle at %q", next)
		}
		seen[next] = struct{}{}
		cursor = next
	}
	sort.Slice(candidates, func(i, j int) bool {
		ti, tj := candidates[i].GetCreatedAt().AsTime(), candidates[j].GetCreatedAt().AsTime()
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return candidates[i].GetID() > candidates[j].GetID()
	})
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.GetID())
	}
	return ids, nil
}

func FailedDoctorResult(profileName, workspaceName string, err error) DoctorResult {
	workspace, _ := ResolveWorkspaceName(workspaceName, profileName)
	return DoctorResult{
		Profile:        normalizedProfileName(profileName),
		Workspace:      workspace,
		Recommendation: err.Error(),
	}
}
