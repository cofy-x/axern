package agent

import (
	"context"
	"fmt"
	"sort"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DeleteResult struct {
	Workspace   string    `json:"workspace"`
	State       string    `json:"state"`
	ServiceID   string    `json:"service_id"`
	ClaimIDs    []string  `json:"claim_ids"`
	Message     string    `json:"message"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

func (Control) DeleteWorkspace(ctx context.Context, client ServiceClient, workspace string, timeout time.Duration) (DeleteResult, error) {
	workspace, err := ResolveWorkspaceName(workspace, workspace)
	if err != nil {
		return DeleteResult{}, err
	}
	services, err := listAgentServices(ctx, client, map[string]string{LabelWorkflow: "agent", LabelWorkspace: workspace})
	if err != nil {
		return DeleteResult{}, err
	}
	if len(services) == 0 {
		return DeleteResult{}, fmt.Errorf("agent workspace %q not found", workspace)
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].GetUpdatedAt().AsTime().After(services[j].GetUpdatedAt().AsTime())
	})
	service := services[0]
	liveCount := 0
	for _, candidate := range services {
		if !serviceDeletionComplete(candidate) {
			liveCount++
			service = candidate
		}
	}
	if liveCount > 1 {
		return DeleteResult{}, fmt.Errorf("agent workspace %q has multiple active services", workspace)
	}
	if !serviceDeletionInProgress(service) && !serviceDeletionComplete(service) {
		service, err = deleteWorkspaceService(ctx, client, workspace, service)
		if err != nil {
			return DeleteResult{}, err
		}
	}
	waitCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	for {
		deletion := service.GetDeletionStatus()
		result := DeleteResult{Workspace: workspace, State: LifecycleDeleting, ServiceID: service.GetID(), ClaimIDs: append([]string(nil), deletion.GetClaimIds()...), Message: deletion.GetMessage()}
		if deletion.GetPhase() == servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE {
			result.State = LifecycleDeleted
			result.CompletedAt = deletion.GetCompletedAt().AsTime()
			return result, nil
		}
		select {
		case <-waitCtx.Done():
			return result, fmt.Errorf("timed out waiting for agent workspace %q physical deletion; deletion continues in the background", workspace)
		case <-time.After(500 * time.Millisecond):
		}
		resp, err := client.GetService(waitCtx, &servicev1.GetServiceRequest{ServiceID: service.GetID()})
		if err != nil {
			return result, err
		}
		service = resp.GetService()
	}
}

func deleteWorkspaceService(ctx context.Context, client ServiceClient, workspace string, service *servicev1.Service) (*servicev1.Service, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if service.GetReplicas() != 0 {
			return nil, fmt.Errorf("agent workspace %q is running; stop it before deletion", workspace)
		}
		resp, err := client.DeleteService(ctx, &servicev1.DeleteServiceRequest{
			ServiceID: service.GetID(), ExpectedVersion: service.GetVersion(), RequireSuspended: true,
			VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE,
		})
		if status.Code(err) == codes.Aborted && attempt == 0 {
			serviceID := service.GetID()
			current, getErr := client.GetService(ctx, &servicev1.GetServiceRequest{ServiceID: serviceID})
			if getErr != nil {
				return nil, getErr
			}
			service = current.GetService()
			if service == nil {
				return nil, fmt.Errorf("agent workspace service %q was not returned", serviceID)
			}
			if serviceDeletionInProgress(service) || serviceDeletionComplete(service) {
				return service, nil
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if resp.GetService() == nil {
			return nil, fmt.Errorf("deleted agent workspace service %q was not returned", service.GetID())
		}
		return resp.GetService(), nil
	}
	return nil, fmt.Errorf("delete agent workspace service %q failed after retry", service.GetID())
}
