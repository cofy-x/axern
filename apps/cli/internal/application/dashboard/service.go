package dashboard

import (
	"context"
	"fmt"
	"strings"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type ServiceReplicaView string

const (
	ServiceReplicaViewCurrent ServiceReplicaView = "current"
	ServiceReplicaViewAll     ServiceReplicaView = "all"
)

func (c Control) Services(ctx context.Context) ([]ServiceDTO, error) {
	resp, err := c.services.List(ctx, &servicev1.ListServicesRequest{})
	if err != nil {
		return nil, err
	}
	out := make([]ServiceDTO, 0, len(resp.GetServices()))
	for _, svc := range resp.GetServices() {
		out = append(out, NewServiceDTO(svc))
	}
	return out, nil
}

func (c Control) ServiceDetail(ctx context.Context, serviceID string) (ServiceDetail, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return ServiceDetail{}, fmt.Errorf("service id is required")
	}
	result, err := c.services.Get(ctx, serviceID)
	if err != nil {
		return ServiceDetail{}, err
	}
	replicas, err := c.ServiceReplicas(ctx, serviceID, ServiceReplicaViewCurrent)
	if err != nil {
		return ServiceDetail{}, err
	}
	events, err := c.ServiceEvents(ctx, serviceID, DefaultEventLimit)
	if err != nil {
		return ServiceDetail{}, err
	}
	return ServiceDetail{
		Service:  ptr(NewServiceDTO(result.Service)),
		Replicas: replicas,
		Events:   events,
	}, nil
}

func (c Control) ServiceReplicas(ctx context.Context, serviceID string, view ServiceReplicaView) ([]ReplicaDTO, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, fmt.Errorf("service id is required")
	}
	replicaView, err := view.toProtoView()
	if err != nil {
		return nil, err
	}
	resp, err := c.services.ListReplicas(ctx, serviceID, &servicev1.ServiceReplicaListFilter{View: replicaView})
	if err != nil {
		return nil, err
	}
	out := make([]ReplicaDTO, 0, len(resp.GetReplicas()))
	for _, replica := range resp.GetReplicas() {
		out = append(out, NewReplicaDTO(replica))
	}
	return out, nil
}

func (c Control) ServiceEvents(ctx context.Context, serviceID string, limit int32) ([]ServiceEvent, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, fmt.Errorf("service id is required")
	}
	resp, err := c.services.ListEvents(ctx, serviceID, NormalizeLimit(limit))
	if err != nil {
		return nil, err
	}
	out := make([]ServiceEvent, 0, len(resp.GetEvents()))
	for _, event := range resp.GetEvents() {
		out = append(out, NewServiceEvent(event))
	}
	return out, nil
}

func ParseServiceReplicaView(value string) (ServiceReplicaView, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ServiceReplicaViewCurrent):
		return ServiceReplicaViewCurrent, nil
	case string(ServiceReplicaViewAll):
		return ServiceReplicaViewAll, nil
	default:
		return "", fmt.Errorf("replica view must be current or all")
	}
}

func (v ServiceReplicaView) toProtoView() (servicev1.ServiceReplicaView, error) {
	switch v {
	case ServiceReplicaViewCurrent:
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT, nil
	case ServiceReplicaViewAll:
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_ALL, nil
	default:
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UNSPECIFIED, fmt.Errorf("replica view must be current or all")
	}
}
