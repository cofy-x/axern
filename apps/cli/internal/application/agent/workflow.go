package agent

import (
	"context"
	"fmt"
	"sort"
	"time"

	appcatalog "github.com/cofy-x/axern/apps/cli/internal/application/catalog"
	appenvironment "github.com/cofy-x/axern/apps/cli/internal/application/environment"
	appservice "github.com/cofy-x/axern/apps/cli/internal/application/service"
	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
	"github.com/cofy-x/axern/lib/go/agentprofile"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (Control) Start(ctx context.Context, params Params) error {
	if params.ServiceClient == nil {
		return fmt.Errorf("service client is required")
	}
	if params.Remote == nil {
		return fmt.Errorf("remote runner is required")
	}
	adapter, err := AdapterFor(params.Profile.Agent)
	if err != nil {
		return err
	}
	if err := adapter.Validate(params.Profile); err != nil {
		return err
	}
	bundle, err := resolveAgentBundle(ctx, params.Catalog, adapter)
	if err != nil {
		return fmt.Errorf("resolve agent bundle: %w", err)
	}
	workspace, err := ResolveWorkspaceName(params.Workspace, params.Profile.Name)
	if err != nil {
		return err
	}
	params.Workspace = workspace
	createCtx := params.CreateContext
	if createCtx == nil {
		createCtx = ctx
	}
	ensured, err := ensureService(createCtx, params, adapter, bundle)
	if err != nil {
		return err
	}
	allocation, err := waitReadyAllocation(ctx, params.ServiceClient, ensured.ServiceID, params.ServiceTimeout)
	if err != nil {
		return err
	}
	proxy, err := startLocalProxy(params.Profile)
	if err != nil {
		return err
	}
	defer proxy.Close()

	target := params.RemoteTarget
	target.AllocationID = allocation.AllocationID
	target.User = firstNonEmpty(target.User, params.Profile.RemoteUser, DefaultRemoteUser)
	configWritten := false
	if params.Profile.RestoreOnExit {
		defer func() {
			if configWritten {
				_ = params.Remote.RestoreAgentConfig(context.Background(), target, params.Profile.Agent)
			}
		}()
	}

	forwardParams := apptunnel.ForwardParams{
		CreateContext:         createCtx,
		AllocationID:          allocation.AllocationID,
		LocalTarget:           proxy.Addr(),
		TTL:                   params.TTL,
		WaitReady:             true,
		ReadyTimeout:          params.ReadyTimeout,
		Relay:                 params.Relay,
		RelayDialer:           params.RelayDialer,
		Connector:             params.Connector,
		OnReconnect:           params.OnReconnect,
		ConnectorReadyTimeout: 10 * time.Second,
		OnSessionCreated: func(session apptunnel.ForwardSession) error {
			if err := params.Remote.WriteAgentConfig(ctx, target, session.Session.GetRemotePort(), params.Profile, proxy.Token()); err != nil {
				return err
			}
			configWritten = true
			result := Result{
				Agent:             string(params.Profile.Agent),
				ProfileName:       normalizedProfileName(params.Profile.Name),
				Workspace:         params.Workspace,
				ServiceID:         ensured.ServiceID,
				CreatedService:    ensured.Created,
				AllocationID:      allocation.AllocationID,
				NodeID:            allocation.NodeID,
				Session:           session.Session,
				RemoteBindAddress: fmt.Sprintf("127.0.0.1:%d", session.Session.GetRemotePort()),
				Upstream:          RedactURLUser(params.Profile.Upstream),
			}
			if params.OnReady != nil {
				return params.OnReady(result)
			}
			return nil
		},
	}
	switch params.Mode {
	case ModeConnect:
	case ModeShell:
		forwardParams.OnConnectorStart = func(session apptunnel.ForwardSession) error {
			env := mergeCommandEnv(params.Profile.Env, adapter.SessionEnv(session.Session.GetRemotePort(), params.Profile, proxy.Token()))
			return params.Remote.Run(ctx, target, commandInWorkspaceWithEnv(env, interactiveShellWithBundlePath(bundle)), true)
		}
	case ModeRun:
		forwardParams.OnConnectorStart = func(session apptunnel.ForwardSession) error {
			command, requestTTY := adapter.RunCommand(bundle.Binary, params.RunArgs)
			env := mergeCommandEnv(params.Profile.Env, adapter.SessionEnv(session.Session.GetRemotePort(), params.Profile, proxy.Token()))
			return params.Remote.Run(ctx, target, commandInWorkspaceWithEnv(env, command), requestTTY)
		}
	default:
		return fmt.Errorf("unsupported agent mode %q", params.Mode)
	}
	return params.Tunnel.Forward(ctx, forwardParams)
}

type ensureResult struct {
	ServiceID string
	Created   bool
}

func ensureService(ctx context.Context, params Params, adapter Adapter, bundle bundleRuntime) (ensureResult, error) {
	profile := normalizedProfileName(params.Profile.Name)
	workspace, err := ResolveWorkspaceName(params.Workspace, profile)
	if err != nil {
		return ensureResult{}, err
	}
	if params.Environment == nil {
		return ensureResult{}, errEnvironmentResolverRequired()
	}
	allWorkspaceServices, err := listAgentServices(ctx, params.ServiceClient, map[string]string{
		LabelWorkflow: "agent", LabelWorkspace: workspace,
	})
	if err != nil {
		return ensureResult{}, err
	}
	for _, service := range allWorkspaceServices {
		if serviceDeletionInProgress(service) {
			return ensureResult{}, fmt.Errorf("agent workspace %q is deleting", workspace)
		}
	}
	workspaceServices := make([]*servicev1.Service, 0, 1)
	for _, service := range allWorkspaceServices {
		if !serviceDeletionComplete(service) {
			workspaceServices = append(workspaceServices, service)
		}
	}
	if len(workspaceServices) > 1 {
		return ensureResult{}, fmt.Errorf("agent workspace %q has multiple active services", workspace)
	}
	runtimeProfile := params.Profile
	namespace := firstNonEmpty(runtimeProfile.Namespace, DefaultNamespace)
	if len(workspaceServices) == 1 {
		namespace = firstNonEmpty(workspaceServices[0].GetNamespace(), DefaultNamespace)
		runtimeProfile.Namespace = namespace
		serviceID := workspaceServices[0].GetID()
		updated, err := updateServiceWithRetry(ctx, params.ServiceClient, serviceID, func(current *servicev1.Service) (*servicev1.UpdateServiceRequest, error) {
			if current.GetReplicas() > 0 {
				if current.GetLabels()[LabelProfile] != profile || current.GetLabels()[LabelAgent] != string(params.Profile.Agent) {
					return nil, fmt.Errorf("agent workspace %q is running with profile %q; stop it before switching to profile %q", workspace, current.GetLabels()[LabelProfile], profile)
				}
				environmentIDs, err := currentEnvironmentIDs(ctx, runtimeProfile, params.Catalog, params.Environment)
				if err != nil {
					return nil, err
				}
				if !containsString(environmentIDs, current.GetEnvironmentID()) || !workspaceConfigMatches(current, bundle) {
					return nil, fmt.Errorf("agent workspace %q runtime changed; stop it before updating the runtime", workspace)
				}
				return nil, nil
			}
			environmentID, err := desiredEnvironmentID(ctx, runtimeProfile, params.Catalog, params.Environment, adapter)
			if err != nil {
				return nil, err
			}
			replicas := DefaultReplicas
			return &servicev1.UpdateServiceRequest{
				ServiceID:       current.GetID(),
				ExpectedVersion: current.GetVersion(),
				Replicas:        &replicas,
				EnvironmentID:   &environmentID,
				Config:          workspaceExecutionConfig(workspace, bundle),
				Labels:          workspaceLabels(workspace, profile, string(params.Profile.Agent)),
				UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"replicas", "environment_id", "config", "labels"}},
			}, nil
		})
		if err != nil {
			return ensureResult{}, err
		}
		if updated == nil {
			return ensureResult{ServiceID: serviceID}, nil
		}
		return ensureResult{ServiceID: updated.GetID()}, nil
	}
	environmentID, err := desiredEnvironmentID(ctx, runtimeProfile, params.Catalog, params.Environment, adapter)
	if err != nil {
		return ensureResult{}, err
	}
	services := appservice.New(params.ServiceClient)
	createResp, err := services.Create(ctx, appservice.CreateParams{
		Namespace:     namespace,
		EnvironmentID: environmentID,
		Replicas:      DefaultReplicas,
		Labels:        workspaceLabels(workspace, profile, string(params.Profile.Agent)),
		Config:        workspaceExecutionConfig(workspace, bundle),
	})
	if err != nil {
		return ensureResult{}, err
	}
	if createResp.GetService().GetID() == "" {
		return ensureResult{}, fmt.Errorf("control plane returned empty service id")
	}
	return ensureResult{ServiceID: createResp.GetService().GetID(), Created: true}, nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func desiredEnvironmentID(ctx context.Context, profile agentprofile.Profile, catalog appcatalog.RuntimeCatalogClient, environment appenvironment.EnvironmentClient, adapter Adapter) (string, error) {
	ids, err := currentEnvironmentIDs(ctx, profile, catalog, environment)
	if err != nil {
		return "", err
	}
	if len(ids) > 0 {
		return ids[0], nil
	}
	namespace := firstNonEmpty(profile.Namespace, DefaultNamespace)
	return appenvironment.New(environment).ResolveID(ctx, appenvironment.ResolveParams{Spec: &environmentv1.EnvironmentSpec{
		Namespace: namespace, TemplateID: firstNonEmpty(profile.TemplateID, adapter.DefaultTemplateID()),
	}})
}

func workspaceLabels(workspace, profile, agent string) map[string]string {
	return map[string]string{
		LabelWorkflow: "agent", LabelWorkspace: workspace, LabelProfile: profile, LabelAgent: agent,
	}
}

func workspaceExecutionConfig(workspace string, bundle bundleRuntime) *commonv1.ExecutionConfig {
	return &commonv1.ExecutionConfig{VolumeMounts: []*commonv1.ServiceVolumeMount{{
		Name: workspaceVolumeName(workspace), Target: DefaultWorkspace, Options: []string{"rw", "nosuid", "nodev"},
		ReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE,
	}}, ImageMounts: []*commonv1.ImageMount{{Image: bundle.Image, Target: bundle.MountTarget, Readonly: true}}}
}

type serviceUpdateBuilder func(*servicev1.Service) (*servicev1.UpdateServiceRequest, error)

func updateServiceWithRetry(ctx context.Context, client ServiceClient, serviceID string, build serviceUpdateBuilder) (*servicev1.Service, error) {
	for attempt := 0; attempt < 2; attempt++ {
		currentResp, err := client.GetService(ctx, &servicev1.GetServiceRequest{ServiceID: serviceID})
		if err != nil {
			return nil, err
		}
		current := currentResp.GetService()
		if current == nil {
			return nil, fmt.Errorf("agent workspace service %q was not returned", serviceID)
		}
		req, err := build(current)
		if err != nil || req == nil {
			return nil, err
		}
		resp, err := client.UpdateService(ctx, req)
		if status.Code(err) == codes.Aborted && attempt == 0 {
			continue
		}
		if err != nil {
			return nil, err
		}
		if resp.GetService() == nil {
			return nil, fmt.Errorf("updated agent workspace service %q was not returned", serviceID)
		}
		return resp.GetService(), nil
	}
	return nil, fmt.Errorf("update agent workspace service %q failed after retry", serviceID)
}

type selectedAllocation struct {
	AllocationID string
	NodeID       string
}

func waitReadyAllocation(ctx context.Context, client ServiceClient, serviceID string, timeout time.Duration) (selectedAllocation, error) {
	waitCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		resp, err := client.ListServiceReplicas(waitCtx, &servicev1.ListServiceReplicasRequest{
			ServiceID: serviceID,
			Filter: &servicev1.ServiceReplicaListFilter{
				View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT,
			},
		})
		if err != nil {
			return selectedAllocation{}, err
		}
		candidates := readyAllocations(resp.GetReplicas())
		if len(candidates) > 0 {
			return candidates[0], nil
		}
		select {
		case <-waitCtx.Done():
			return selectedAllocation{}, fmt.Errorf("service %s has no current ready replicas before timeout", serviceID)
		case <-ticker.C:
		}
	}
}

func readyAllocations(replicas []*servicev1.ServiceReplica) []selectedAllocation {
	out := make([]selectedAllocation, 0, len(replicas))
	for _, replica := range replicas {
		if replica == nil || replica.GetID() == "" {
			continue
		}
		if !replica.GetReady() || replica.GetEnded() || replica.GetOutdated() {
			continue
		}
		out = append(out, selectedAllocation{AllocationID: replica.GetID(), NodeID: replica.GetNodeID()})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AllocationID < out[j].AllocationID
	})
	return out
}
