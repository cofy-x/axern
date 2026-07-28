package service

import (
	"context"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type GetResult struct {
	Service     *servicev1.Service
	LatestEvent *servicev1.ServiceEvent
}

type AllocationCandidate struct {
	ID     string
	NodeID string
	Ready  bool
}

func (c Control) GetService(ctx context.Context, serviceID string) (*servicev1.GetServiceResponse, error) {
	return c.client.GetService(ctx, &servicev1.GetServiceRequest{ServiceID: serviceID})
}

func (c Control) Get(ctx context.Context, serviceID string) (GetResult, error) {
	resp, err := c.client.GetService(ctx, &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		return GetResult{}, err
	}
	result := GetResult{Service: resp.GetService()}
	eventsResp, err := c.client.ListServiceEvents(ctx, &servicev1.ListServiceEventsRequest{
		ServiceID: serviceID,
		Limit:     1,
	})
	if err == nil && len(eventsResp.GetEvents()) > 0 {
		result.LatestEvent = eventsResp.GetEvents()[0]
	}
	return result, nil
}

func (c Control) List(ctx context.Context, req *servicev1.ListServicesRequest) (*servicev1.ListServicesResponse, error) {
	return c.client.ListServices(ctx, req)
}

func (c Control) Update(ctx context.Context, req *servicev1.UpdateServiceRequest) (*servicev1.UpdateServiceResponse, error) {
	return c.client.UpdateService(ctx, req)
}

func (c Control) ListReplicas(ctx context.Context, serviceID string, filter *servicev1.ServiceReplicaListFilter) (*servicev1.ListServiceReplicasResponse, error) {
	return c.client.ListServiceReplicas(ctx, &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
		Filter:    filter,
	})
}

func (c Control) CurrentReadyAllocationCandidates(ctx context.Context, serviceID string) ([]AllocationCandidate, error) {
	resp, err := c.ListReplicas(ctx, serviceID, &servicev1.ServiceReplicaListFilter{
		View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT,
	})
	if err != nil {
		return nil, err
	}
	return CurrentReadyAllocationCandidates(resp.GetReplicas()), nil
}

func (c Control) ListEvents(ctx context.Context, serviceID string, limit int32) (*servicev1.ListServiceEventsResponse, error) {
	return c.client.ListServiceEvents(ctx, &servicev1.ListServiceEventsRequest{
		ServiceID: serviceID,
		Limit:     limit,
	})
}

func CurrentReadyAllocationCandidates(replicas []*servicev1.ServiceReplica) []AllocationCandidate {
	candidates := make([]AllocationCandidate, 0, len(replicas))
	for _, replica := range replicas {
		if replica == nil || replica.GetID() == "" {
			continue
		}
		if !replica.GetReady() || replica.GetEnded() || replica.GetOutdated() {
			continue
		}
		candidates = append(candidates, AllocationCandidate{
			ID:     replica.GetID(),
			NodeID: replica.GetNodeID(),
			Ready:  replica.GetReady(),
		})
	}
	return candidates
}
