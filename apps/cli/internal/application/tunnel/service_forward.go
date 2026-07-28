package tunnel

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc"
)

type ServiceClient interface {
	ListServiceReplicas(context.Context, *servicev1.ListServiceReplicasRequest, ...grpc.CallOption) (*servicev1.ListServiceReplicasResponse, error)
}

type ServiceForwardParams struct {
	CreateContext    context.Context
	ServiceID        string
	AllocationID     string
	NodeID           string
	LocalTarget      string
	TTL              time.Duration
	ReadyTimeout     time.Duration
	Relay            RelayDialConfig
	Connector        ConnectorConfig
	DisableRenew     bool
	ConnectorRunner  ConnectorRunner
	RelayDialer      RelayPeerDialer
	OnReconnect      ConnectorReconnectReporter
	OnSessionCreated func(ServiceForwardSession) error
}

type ServiceForwardSession struct {
	ServiceID         string
	AllocationID      string
	NodeID            string
	SelectionReason   string
	ReadyReplicaCount int
	Session           *tunnelcontrolv1.TunnelSession
	ClientToken       string
}

type ServiceAllocationSelection struct {
	ServiceID         string
	AllocationID      string
	NodeID            string
	Reason            string
	ReadyReplicaCount int
}

func (c Control) ForwardService(ctx context.Context, serviceClient ServiceClient, params ServiceForwardParams) error {
	createCtx := params.CreateContext
	if createCtx == nil {
		createCtx = ctx
	}
	selection, err := SelectReadyServiceAllocation(createCtx, serviceClient, ServiceAllocationSelectionParams{
		ServiceID:    params.ServiceID,
		AllocationID: params.AllocationID,
		NodeID:       params.NodeID,
	})
	if err != nil {
		return err
	}
	return c.Forward(ctx, ForwardParams{
		CreateContext:   params.CreateContext,
		AllocationID:    selection.AllocationID,
		LocalTarget:     params.LocalTarget,
		TTL:             params.TTL,
		WaitReady:       true,
		ReadyTimeout:    params.ReadyTimeout,
		Relay:           params.Relay,
		Connector:       params.Connector,
		DisableRenew:    params.DisableRenew,
		ConnectorRunner: params.ConnectorRunner,
		RelayDialer:     params.RelayDialer,
		OnReconnect:     params.OnReconnect,
		OnSessionCreated: func(session ForwardSession) error {
			if params.OnSessionCreated == nil {
				return nil
			}
			return params.OnSessionCreated(ServiceForwardSession{
				ServiceID:         selection.ServiceID,
				AllocationID:      selection.AllocationID,
				NodeID:            selection.NodeID,
				SelectionReason:   selection.Reason,
				ReadyReplicaCount: selection.ReadyReplicaCount,
				Session:           session.Session,
				ClientToken:       session.ClientToken,
			})
		},
	})
}

type ServiceAllocationSelectionParams struct {
	ServiceID    string
	AllocationID string
	NodeID       string
}

func SelectReadyServiceAllocation(ctx context.Context, serviceClient ServiceClient, params ServiceAllocationSelectionParams) (ServiceAllocationSelection, error) {
	serviceID := strings.TrimSpace(params.ServiceID)
	if serviceID == "" {
		return ServiceAllocationSelection{}, fmt.Errorf("service id is required")
	}
	resp, err := serviceClient.ListServiceReplicas(ctx, &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
		Filter: &servicev1.ServiceReplicaListFilter{
			View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT,
		},
	})
	if err != nil {
		return ServiceAllocationSelection{}, err
	}
	candidates := readyAllocationCandidates(serviceID, resp.GetReplicas())
	requestedAllocationID := strings.TrimSpace(params.AllocationID)
	requestedNodeID := strings.TrimSpace(params.NodeID)
	if requestedAllocationID != "" && requestedNodeID != "" {
		return ServiceAllocationSelection{}, fmt.Errorf("--allocation-id and --node-id cannot be combined")
	}
	if requestedAllocationID != "" {
		for _, candidate := range candidates {
			if candidate.AllocationID == requestedAllocationID {
				candidate.Reason = "explicit allocation id"
				return candidate, nil
			}
		}
		return ServiceAllocationSelection{}, fmt.Errorf("allocation %s is not a current ready replica for service %s", requestedAllocationID, serviceID)
	}
	if requestedNodeID != "" {
		for _, candidate := range candidates {
			if candidate.NodeID == requestedNodeID {
				candidate.Reason = "explicit node id"
				return candidate, nil
			}
		}
		return ServiceAllocationSelection{}, fmt.Errorf("node %s has no current ready replica for service %s", requestedNodeID, serviceID)
	}
	if len(candidates) == 0 {
		return ServiceAllocationSelection{}, fmt.Errorf("service %s has no current ready replicas", serviceID)
	}
	candidates[0].Reason = "stable first ready allocation"
	return candidates[0], nil
}

func readyAllocationIDs(replicas []*servicev1.ServiceReplica) []string {
	candidates := readyAllocationCandidates("", replicas)
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.AllocationID)
	}
	return ids
}

func readyAllocationCandidates(serviceID string, replicas []*servicev1.ServiceReplica) []ServiceAllocationSelection {
	ids := make([]string, 0, len(replicas))
	byID := map[string]ServiceAllocationSelection{}
	var count int
	for _, replica := range replicas {
		if replica == nil || replica.GetID() == "" {
			continue
		}
		if !replica.GetReady() || replica.GetEnded() || replica.GetOutdated() {
			continue
		}
		count++
		id := replica.GetID()
		ids = append(ids, id)
		byID[id] = ServiceAllocationSelection{
			ServiceID:    firstNonEmpty(serviceID, replica.GetServiceID()),
			AllocationID: id,
			NodeID:       replica.GetNodeID(),
		}
	}
	sort.Strings(ids)
	out := make([]ServiceAllocationSelection, 0, len(ids))
	for _, id := range ids {
		candidate := byID[id]
		candidate.ReadyReplicaCount = count
		out = append(out, candidate)
	}
	return out
}
