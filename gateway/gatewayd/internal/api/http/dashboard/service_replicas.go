package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"google.golang.org/grpc"
)

type ServiceReplicaResolver interface {
	CurrentReadyReplicas(context.Context, string) ([]serviceReplicaCandidate, error)
}

type GatewayControlClient interface {
	ResolveServiceReplicaTargets(context.Context, *gatewayv1.ResolveServiceReplicaTargetsRequest, ...grpc.CallOption) (*gatewayv1.ResolveServiceReplicaTargetsResponse, error)
}

type controlServiceReplicaResolver struct {
	client GatewayControlClient
}

func NewServiceReplicaResolver(client GatewayControlClient) ServiceReplicaResolver {
	return controlServiceReplicaResolver{client: client}
}

func (r controlServiceReplicaResolver) CurrentReadyReplicas(ctx context.Context, serviceID string) ([]serviceReplicaCandidate, error) {
	resp, err := r.client.ResolveServiceReplicaTargets(ctx, &gatewayv1.ResolveServiceReplicaTargetsRequest{ServiceID: serviceID})
	if err != nil {
		return nil, err
	}
	replicas := make([]serviceReplicaCandidate, 0, len(resp.GetReplicas()))
	for _, replica := range resp.GetReplicas() {
		if replica != nil && replica.GetAllocationID() != "" {
			replicas = append(replicas, serviceReplicaCandidate{AllocationID: replica.GetAllocationID(), NodeID: replica.GetNodeID()})
		}
	}
	return replicas, nil
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
