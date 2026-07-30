package appgateway

import (
	"context"
	"strconv"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type LeaseIssuer interface {
	IssueExecutionLease(ctx context.Context, allocationID string, attempt int64, leaseType commonv1.LeaseType, ttl time.Duration, now time.Time) (*commonv1.ExecutionLease, error)
}

type RouteReader interface {
	LoadService(ctx context.Context, serviceID string) (*servicev1.Service, error)
	LoadAllocation(ctx context.Context, allocationID string) (*Allocation, error)
	ReadyServiceEndpoints(ctx context.Context, serviceID string) ([]EndpointTarget, error)
}

type Allocation struct {
	AllocationID string
	OwnerType    string
	OwnerID      string
	NodeID       string
	NodeTarget   string
	Attempt      int64
	Status       commonv1.AllocationStatus
	Ready        bool
}

type EndpointTarget struct {
	AllocationID string
	NodeID       string
	NodeTarget   string
	Attempt      int64
	Ready        bool
}

type Resolver struct {
	routes RouteReader
	leases LeaseIssuer
}

func NewResolver(routes RouteReader, leases LeaseIssuer) *Resolver {
	return &Resolver{routes: routes, leases: leases}
}

func (r *Resolver) ResolveServiceRoute(ctx context.Context, req *gatewayv1.ResolveServiceRouteRequest, ttl time.Duration, now time.Time) (*gatewayv1.ResolveServiceRouteResponse, error) {
	if r == nil || r.routes == nil || r.leases == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "gateway route resolver is not configured")
	}
	namespace := strings.TrimSpace(req.GetNamespace())
	serviceID := strings.TrimSpace(req.GetServiceID())
	portRef := strings.TrimSpace(req.GetPortRef())
	if namespace == "" || serviceID == "" || portRef == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "namespace, service_id, and port_ref are required")
	}
	service, err := r.routes.LoadService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	if service.GetNamespace() != namespace {
		return nil, grpcstatus.Error(codes.NotFound, "service not found")
	}
	port, err := ResolvePort(service.GetConfig().GetPorts(), portRef)
	if err != nil {
		return nil, err
	}
	targets, err := r.routes.ReadyServiceEndpoints(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	endpoints := make([]*gatewayv1.ServiceRouteEndpoint, 0, len(targets))
	for _, target := range targets {
		lease, err := r.leases.IssueExecutionLease(ctx, target.AllocationID, target.Attempt, commonv1.LeaseType_LEASE_TYPE_SERVICE, ttl, now)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, &gatewayv1.ServiceRouteEndpoint{
			AllocationID:  target.AllocationID,
			NodeID:        target.NodeID,
			NodeTarget:    target.NodeTarget,
			Attempt:       target.Attempt,
			Ready:         target.Ready,
			ContainerPort: port.GetContainerPort(),
			Protocol:      port.GetProtocol(),
			Lease:         lease,
		})
	}
	if len(endpoints) == 0 {
		return nil, grpcstatus.Error(codes.Unavailable, "service has no ready endpoints")
	}
	return &gatewayv1.ResolveServiceRouteResponse{
		ServiceID:     service.GetID(),
		Namespace:     service.GetNamespace(),
		ServiceStatus: service.GetStatus(),
		Port:          port,
		Endpoints:     endpoints,
	}, nil
}

func (r *Resolver) ResolveAllocationTerminal(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest, ttl time.Duration, now time.Time) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	if r == nil || r.routes == nil || r.leases == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "gateway terminal resolver is not configured")
	}
	allocationID := strings.TrimSpace(req.GetAllocationID())
	if allocationID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	alloc, err := r.routes.LoadAllocation(ctx, allocationID)
	if err != nil {
		return nil, err
	}
	if alloc.Status != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "allocation is not running")
	}
	leaseType := commonv1.LeaseType_LEASE_TYPE_RUN
	if alloc.OwnerType == allocationkernel.OwnerService {
		leaseType = commonv1.LeaseType_LEASE_TYPE_SERVICE
	}
	lease, err := r.leases.IssueExecutionLease(ctx, alloc.AllocationID, alloc.Attempt, leaseType, ttl, now)
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: alloc.AllocationID,
		OwnerType:    alloc.OwnerType,
		OwnerID:      alloc.OwnerID,
		NodeID:       alloc.NodeID,
		NodeTarget:   alloc.NodeTarget,
		Attempt:      alloc.Attempt,
		Lease:        lease,
	}, nil
}

func (r *Resolver) ResolveServiceReplicaTargets(ctx context.Context, serviceID string) (*gatewayv1.ResolveServiceReplicaTargetsResponse, error) {
	if r == nil || r.routes == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "gateway route resolver is not configured")
	}
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "service_id is required")
	}
	if _, err := r.routes.LoadService(ctx, serviceID); err != nil {
		return nil, err
	}
	targets, err := r.routes.ReadyServiceEndpoints(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	response := &gatewayv1.ResolveServiceReplicaTargetsResponse{Replicas: make([]*gatewayv1.ServiceReplicaTarget, 0, len(targets))}
	for _, target := range targets {
		response.Replicas = append(response.Replicas, &gatewayv1.ServiceReplicaTarget{AllocationID: target.AllocationID, NodeID: target.NodeID})
	}
	return response, nil
}

func ResolvePort(ports []*commonv1.PortSpec, ref string) (*gatewayv1.ServiceRoutePort, error) {
	for _, port := range ports {
		if port == nil {
			continue
		}
		if strings.TrimSpace(port.GetName()) != "" && strings.TrimSpace(port.GetName()) == ref {
			return routePort(port), nil
		}
	}
	number, err := strconv.Atoi(ref)
	if err != nil || number <= 0 || number > 65535 {
		return nil, grpcstatus.Error(codes.NotFound, "service port not found")
	}
	for _, port := range ports {
		if port != nil && int(port.GetContainerPort()) == number {
			return routePort(port), nil
		}
	}
	return &gatewayv1.ServiceRoutePort{
		Protocol:      commonv1.PortProtocol_PORT_PROTOCOL_TCP,
		ContainerPort: int32(number),
	}, nil
}

func routePort(port *commonv1.PortSpec) *gatewayv1.ServiceRoutePort {
	protocol := port.GetProtocol()
	if protocol == commonv1.PortProtocol_PORT_PROTOCOL_UNSPECIFIED {
		protocol = commonv1.PortProtocol_PORT_PROTOCOL_TCP
	}
	return &gatewayv1.ServiceRoutePort{
		Name:          port.GetName(),
		Protocol:      protocol,
		ContainerPort: port.GetContainerPort(),
	}
}
