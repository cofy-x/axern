package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

const agentListPageSize = 100

const (
	LifecycleStarting  = "starting"
	LifecycleRunning   = "running"
	LifecycleSuspended = "suspended"
	LifecycleDegraded  = "degraded"
	LifecycleDeleting  = "deleting"
	LifecycleDeleted   = "deleted"
)

type RuntimeSummary struct {
	Workspace      string `json:"workspace"`
	LifecycleState string `json:"lifecycle_state"`
	Persistent     bool   `json:"persistent"`
	ServiceID      string `json:"service_id"`
	Profile        string `json:"profile"`
	Agent          string `json:"agent"`
	Namespace      string `json:"namespace"`
	Ready          int32  `json:"ready_replicas"`
	Desired        int32  `json:"desired_replicas"`
	EnvironmentID  string `json:"environment_id"`
}

type StopResult struct {
	Workspace      string `json:"workspace"`
	ServiceID      string `json:"service_id,omitempty"`
	LifecycleState string `json:"lifecycle_state"`
}

func (Control) ListRuntimes(ctx context.Context, client ServiceClient, workspace, profile string) ([]RuntimeSummary, error) {
	labels := map[string]string{LabelWorkflow: "agent"}
	if workspace = strings.TrimSpace(workspace); workspace != "" {
		var err error
		workspace, err = ResolveWorkspaceName(workspace, workspace)
		if err != nil {
			return nil, err
		}
		labels[LabelWorkspace] = workspace
	}
	if profile = strings.TrimSpace(profile); profile != "" {
		labels[LabelProfile] = profile
	}
	services, err := listAgentServices(ctx, client, labels)
	if err != nil {
		return nil, err
	}
	result := make([]RuntimeSummary, 0, len(services))
	for _, service := range services {
		if service.GetLabels()[LabelWorkspace] == "" {
			continue
		}
		if serviceDeletionComplete(service) {
			continue
		}
		result = append(result, RuntimeSummary{
			Workspace:      service.GetLabels()[LabelWorkspace],
			LifecycleState: workspaceLifecycleState(service),
			Persistent:     workspaceConfigShapeMatches(service),
			ServiceID:      service.GetID(),
			Profile:        service.GetLabels()[LabelProfile],
			Agent:          service.GetLabels()[LabelAgent],
			Namespace:      service.GetNamespace(),
			Ready:          service.GetReadyReplicas(),
			Desired:        service.GetReplicas(),
			EnvironmentID:  service.GetEnvironmentID(),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Workspace != result[j].Workspace {
			return result[i].Workspace < result[j].Workspace
		}
		return result[i].ServiceID < result[j].ServiceID
	})
	return result, nil
}

func (Control) Stop(ctx context.Context, client ServiceClient, workspace string) (StopResult, error) {
	var err error
	workspace, err = ResolveWorkspaceName(workspace, workspace)
	if err != nil {
		return StopResult{}, err
	}
	services, err := listAgentServices(ctx, client, map[string]string{
		LabelWorkflow: "agent", LabelWorkspace: workspace,
	})
	if err != nil {
		return StopResult{}, err
	}
	services = nonTerminalWorkspaceServices(services)
	result := StopResult{Workspace: workspace, LifecycleState: LifecycleSuspended}
	if len(services) == 0 {
		return StopResult{}, fmt.Errorf("agent workspace %q not found", workspace)
	}
	if len(services) > 1 {
		return StopResult{}, fmt.Errorf("agent workspace %q has multiple active services", workspace)
	}
	if serviceDeletionInProgress(services[0]) {
		return StopResult{}, fmt.Errorf("agent workspace %q is deleting", workspace)
	}
	serviceID := services[0].GetID()
	result.ServiceID = serviceID
	updated, err := updateServiceWithRetry(ctx, client, serviceID, func(current *servicev1.Service) (*servicev1.UpdateServiceRequest, error) {
		if current.GetReplicas() == 0 {
			return nil, nil
		}
		replicas := int32(0)
		return &servicev1.UpdateServiceRequest{
			ServiceID: current.GetID(), ExpectedVersion: current.GetVersion(), Replicas: &replicas,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"replicas"}},
		}, nil
	})
	if err != nil {
		return StopResult{}, fmt.Errorf("stop agent workspace %q: %w", workspace, err)
	}
	if updated != nil {
		result.ServiceID = updated.GetID()
	}
	return result, nil
}

func workspaceLifecycleState(service *servicev1.Service) string {
	if serviceDeletionInProgress(service) {
		return LifecycleDeleting
	}
	if service.GetReplicas() == 0 {
		return LifecycleSuspended
	}
	if service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED ||
		service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_FAILED || service.GetUnhealthyReplicas() > 0 {
		return LifecycleDegraded
	}
	if service.GetReadyReplicas() >= service.GetReplicas() {
		return LifecycleRunning
	}
	return LifecycleStarting
}

func serviceDeletionInProgress(service *servicev1.Service) bool {
	phase := service.GetDeletionStatus().GetPhase()
	return phase != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_UNSPECIFIED && phase != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE
}

func serviceDeletionComplete(service *servicev1.Service) bool {
	return service.GetDeletionStatus().GetPhase() == servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE
}

func nonTerminalWorkspaceServices(services []*servicev1.Service) []*servicev1.Service {
	out := make([]*servicev1.Service, 0, len(services))
	for _, service := range services {
		if service != nil && !serviceDeletionComplete(service) {
			out = append(out, service)
		}
	}
	return out
}

func listAgentServices(ctx context.Context, client ServiceClient, labels map[string]string) ([]*servicev1.Service, error) {
	if client == nil {
		return nil, fmt.Errorf("service client is required")
	}
	resp, err := client.ListServices(ctx, &servicev1.ListServicesRequest{Filter: &servicev1.ServiceListFilter{
		Labels: labels,
		Statuses: []servicev1.ServiceStatus{
			servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
			servicev1.ServiceStatus_SERVICE_STATUS_READY,
			servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
			servicev1.ServiceStatus_SERVICE_STATUS_FAILED,
			servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
			servicev1.ServiceStatus_SERVICE_STATUS_DELETED,
		},
	}})
	if err != nil {
		return nil, err
	}
	out := make([]*servicev1.Service, 0)
	for _, service := range resp.GetServices() {
		matched := service != nil
		for key, value := range labels {
			matched = matched && service.GetLabels()[key] == value
		}
		if matched {
			out = append(out, service)
		}
	}
	return out, nil
}

func workspaceConfigMatches(service *servicev1.Service, bundle bundleRuntime) bool {
	workspace := service.GetLabels()[LabelWorkspace]
	if workspace == "" {
		return false
	}
	actual := service.GetConfig().GetVolumeMounts()
	expectedConfig := workspaceExecutionConfig(workspace, bundle)
	expected := expectedConfig.GetVolumeMounts()
	actualImages := service.GetConfig().GetImageMounts()
	expectedImages := expectedConfig.GetImageMounts()
	return len(actual) == 1 && len(expected) == 1 && proto.Equal(actual[0], expected[0]) &&
		len(actualImages) == 1 && len(expectedImages) == 1 && proto.Equal(actualImages[0], expectedImages[0])
}

func workspaceConfigShapeMatches(service *servicev1.Service) bool {
	if service == nil || service.GetLabels()[LabelWorkspace] == "" {
		return false
	}
	volumes := service.GetConfig().GetVolumeMounts()
	images := service.GetConfig().GetImageMounts()
	return len(volumes) == 1 && len(images) == 1 && images[0].GetReadonly()
}

func listActiveAgentServices(ctx context.Context, client ServiceClient, namespace string, labels map[string]string) ([]*servicev1.Service, error) {
	if client == nil {
		return nil, fmt.Errorf("service client is required")
	}
	namespace = strings.TrimSpace(namespace)
	statuses := []servicev1.ServiceStatus{
		servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
		servicev1.ServiceStatus_SERVICE_STATUS_READY,
		servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		servicev1.ServiceStatus_SERVICE_STATUS_FAILED,
	}
	if namespace == "" {
		// An empty namespace is the API's all-namespaces selector. Filter Agent
		// Workspace identity locally after the global query.
		resp, err := client.ListServices(ctx, &servicev1.ListServicesRequest{})
		if err != nil {
			return nil, err
		}
		services := make([]*servicev1.Service, 0, len(resp.GetServices()))
		for _, service := range resp.GetServices() {
			if activeAgentServiceMatches(service, statuses, labels) {
				services = append(services, service)
			}
		}
		return services, nil
	}
	cursor := ""
	seen := map[string]struct{}{}
	services := make([]*servicev1.Service, 0)
	for {
		resp, err := client.ListServices(ctx, &servicev1.ListServicesRequest{Filter: &servicev1.ServiceListFilter{
			Namespace: namespace,
			Statuses:  statuses,
			Labels:    labels, Cursor: cursor, PageSize: agentListPageSize,
		}})
		if err != nil {
			return nil, err
		}
		services = append(services, resp.GetServices()...)
		next := strings.TrimSpace(resp.GetNextCursor())
		if next == "" {
			return services, nil
		}
		if next == cursor {
			return nil, fmt.Errorf("service list returned repeated cursor %q", next)
		}
		if _, ok := seen[next]; ok {
			return nil, fmt.Errorf("service list returned cursor cycle at %q", next)
		}
		seen[next] = struct{}{}
		cursor = next
	}
}

func activeAgentServiceMatches(service *servicev1.Service, statuses []servicev1.ServiceStatus, labels map[string]string) bool {
	if service == nil {
		return false
	}
	statusMatched := false
	for _, candidate := range statuses {
		if service.GetStatus() == candidate {
			statusMatched = true
			break
		}
	}
	if !statusMatched {
		return false
	}
	for key, value := range labels {
		if service.GetLabels()[key] != value {
			return false
		}
	}
	return true
}
