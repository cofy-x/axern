package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc"
)

type ServiceReplicaResolver interface {
	CurrentReadyReplicas(context.Context, string) ([]serviceReplicaCandidate, error)
}

type ServiceControlClient interface {
	ListServiceReplicas(context.Context, *servicev1.ListServiceReplicasRequest, ...grpc.CallOption) (*servicev1.ListServiceReplicasResponse, error)
}

type controlServiceReplicaResolver struct {
	client ServiceControlClient
}

func NewServiceReplicaResolver(client ServiceControlClient) ServiceReplicaResolver {
	return controlServiceReplicaResolver{client: client}
}

func (r controlServiceReplicaResolver) CurrentReadyReplicas(ctx context.Context, serviceID string) ([]serviceReplicaCandidate, error) {
	resp, err := r.client.ListServiceReplicas(ctx, &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
		Filter: &servicev1.ServiceReplicaListFilter{
			View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT,
		},
	})
	if err != nil {
		return nil, err
	}
	return currentReadyReplicas(resp.GetReplicas()), nil
}

type serviceReplicaResponse struct {
	ServiceID string                    `json:"service_id"`
	Replicas  []serviceReplicaCandidate `json:"replicas"`
}

type serviceReplicaCandidate struct {
	AllocationID string `json:"allocation_id"`
	NodeID       string `json:"node_id,omitempty"`
}

func (h *Handler) serveServiceReplicas(w http.ResponseWriter, r *http.Request) {
	if h.resolver == nil {
		http.Error(w, "service replica resolver unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serviceID, ok := parseServiceReplicaPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	replicas, err := h.resolver.CurrentReadyReplicas(r.Context(), serviceID)
	if err != nil {
		http.Error(w, "service replicas unavailable", http.StatusBadGateway)
		return
	}
	writeJSON(w, serviceReplicaResponse{
		ServiceID: serviceID,
		Replicas:  replicas,
	})
}

func currentReadyReplicas(replicas []*servicev1.ServiceReplica) []serviceReplicaCandidate {
	out := make([]serviceReplicaCandidate, 0, len(replicas))
	for _, replica := range replicas {
		if replica == nil || replica.GetID() == "" {
			continue
		}
		if !replica.GetReady() || replica.GetEnded() || replica.GetOutdated() {
			continue
		}
		out = append(out, serviceReplicaCandidate{
			AllocationID: replica.GetID(),
			NodeID:       replica.GetNodeID(),
		})
	}
	return out
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}

func parseServiceReplicaPath(path string) (string, bool) {
	rest := strings.TrimPrefix(path, "/dashboard/api/services/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "replicas" {
		return "", false
	}
	serviceID := strings.TrimSpace(parts[0])
	if serviceID == "" || strings.Contains(serviceID, "/") {
		return "", false
	}
	return serviceID, true
}
