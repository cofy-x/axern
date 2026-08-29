package publicv1

import (
	"context"
	"errors"
	"sort"
	"strings"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) CreateService(ctx context.Context, req *servicev1.CreateServiceRequest) (*servicev1.CreateServiceResponse, error) {
	environmentID := strings.TrimSpace(req.GetEnvironmentID())
	ctx, op := publicOps.Service(ctx, ctrlobs.SpanServiceCreate, publicActionCreate)
	var opErr error
	defer func() { op.End(opErr) }()
	if environmentID == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "environment_id is required")
		return nil, opErr
	}
	if req.GetReplicas() < 0 {
		opErr = grpcstatus.Error(codes.InvalidArgument, "replicas must be non-negative")
		return nil, opErr
	}
	if err := validateExecutionConfigSecretRefs(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateOptionalExecutionArgv(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigResources(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigNetwork(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigCapabilities(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigImageMounts(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateServiceVolumeMounts(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateServiceReadinessProbe(req.GetReadinessProbe()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateServiceLivenessProbe(req.GetLivenessProbe()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateServiceAutoscalingPolicy(req.GetAutoscalingPolicy()); err != nil {
		opErr = err
		return nil, err
	}
	if _, err := s.deps.Environments.GetEnvironment(ctx, environmentID); err != nil {
		opErr = err
		return nil, err
	}
	svc, err := s.deps.Services.Create(ctx, servicekernel.CreateParams{
		Namespace:      req.GetNamespace(),
		EnvironmentID:  environmentID,
		Replicas:       req.GetReplicas(),
		Config:         req.GetConfig(),
		Labels:         req.GetLabels(),
		RolloutPolicy:  req.GetRolloutPolicy(),
		ReadinessProbe: req.GetReadinessProbe(),
		LivenessProbe:  req.GetLivenessProbe(),
		Autoscaling:    req.GetAutoscalingPolicy(),
	}, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	op.SetAttributes(attribute.String(sdkobs.AttrServiceID, svc.GetID()))
	return &servicev1.CreateServiceResponse{Service: svc}, nil
}

func (s *Server) GetService(ctx context.Context, req *servicev1.GetServiceRequest) (*servicev1.GetServiceResponse, error) {
	svc, ok, err := s.deps.Services.Get(ctx, strings.TrimSpace(req.GetServiceID()))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, grpcstatus.Error(codes.NotFound, "service not found")
	}
	return &servicev1.GetServiceResponse{Service: svc}, nil
}

func (s *Server) WatchService(req *servicev1.WatchServiceRequest, stream servicev1.ServiceControl_WatchServiceServer) error {
	serviceID := strings.TrimSpace(req.GetServiceID())
	if serviceID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "service_id is required")
	}
	if req.GetAfterVersion() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "after_version must be non-negative")
	}
	if s.deps.ServiceWatcher == nil {
		return grpcstatus.Error(codes.FailedPrecondition, "service watch is not configured")
	}
	watch, err := s.deps.ServiceWatcher.Watch(stream.Context(), serviceID, req.GetAfterVersion())
	if err != nil {
		return err
	}
	defer watch.Close()
	for {
		service, err := watch.Next(stream.Context())
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&servicev1.WatchServiceResponse{Service: service}); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
}

func (s *Server) GetServiceReplica(ctx context.Context, req *servicev1.GetServiceReplicaRequest) (*servicev1.GetServiceReplicaResponse, error) {
	replica, ok, err := s.deps.Services.GetReplica(ctx, strings.TrimSpace(req.GetServiceID()), strings.TrimSpace(req.GetReplicaID()))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, grpcstatus.Error(codes.NotFound, "service replica not found")
	}
	return &servicev1.GetServiceReplicaResponse{Replica: replica}, nil
}

func (s *Server) ListServices(ctx context.Context, req *servicev1.ListServicesRequest) (*servicev1.ListServicesResponse, error) {
	out, err := s.deps.Services.List(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].GetCreatedAt().AsTime().After(out[j].GetCreatedAt().AsTime())
	})
	return &servicev1.ListServicesResponse{Services: out}, nil
}

func (s *Server) ListServiceReplicas(ctx context.Context, req *servicev1.ListServiceReplicasRequest) (*servicev1.ListServiceReplicasResponse, error) {
	serviceID := strings.TrimSpace(req.GetServiceID())
	ctx, op := publicOps.Service(ctx, ctrlobs.SpanServiceListReplicas, publicActionListReplicas, withServiceID(serviceID))
	var opErr error
	defer func() { op.End(opErr) }()
	if _, ok, err := s.deps.Services.Get(ctx, serviceID); err != nil {
		opErr = err
		return nil, err
	} else if !ok {
		opErr = grpcstatus.Error(codes.NotFound, "service not found")
		return nil, opErr
	}
	replicas, err := s.deps.Services.ListReplicas(ctx, serviceID, req.GetFilter())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &servicev1.ListServiceReplicasResponse{Replicas: replicas}, nil
}

func (s *Server) ListServiceEvents(ctx context.Context, req *servicev1.ListServiceEventsRequest) (*servicev1.ListServiceEventsResponse, error) {
	serviceID := strings.TrimSpace(req.GetServiceID())
	ctx, op := publicOps.Service(ctx, ctrlobs.SpanServiceListEvents, publicActionListEvents, withServiceID(serviceID))
	var opErr error
	defer func() { op.End(opErr) }()
	if _, ok, err := s.deps.Services.Get(ctx, serviceID); err != nil {
		opErr = err
		return nil, err
	} else if !ok {
		opErr = grpcstatus.Error(codes.NotFound, "service not found")
		return nil, opErr
	}
	events, err := s.deps.Services.ListEvents(ctx, serviceID, req.GetLimit())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &servicev1.ListServiceEventsResponse{Events: events}, nil
}

func (s *Server) UpdateService(ctx context.Context, req *servicev1.UpdateServiceRequest) (*servicev1.UpdateServiceResponse, error) {
	serviceID := strings.TrimSpace(req.GetServiceID())
	ctx, op := publicOps.Service(ctx, ctrlobs.SpanServiceUpdate, publicActionUpdate, withServiceID(serviceID))
	var opErr error
	defer func() { op.End(opErr) }()
	if err := validateExecutionConfigSecretRefs(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateOptionalExecutionArgv(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigResources(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigNetwork(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigCapabilities(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigImageMounts(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateServiceVolumeMounts(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateServiceReadinessProbe(req.GetReadinessProbe()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateServiceLivenessProbe(req.GetLivenessProbe()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateServiceAutoscalingPolicy(req.GetAutoscalingPolicy()); err != nil {
		opErr = err
		return nil, err
	}
	if req.EnvironmentID != nil {
		environmentID := strings.TrimSpace(req.GetEnvironmentID())
		if environmentID == "" {
			opErr = grpcstatus.Error(codes.InvalidArgument, "environment_id is required")
			return nil, opErr
		}
		if _, err := s.deps.Environments.GetEnvironment(ctx, environmentID); err != nil {
			opErr = err
			return nil, err
		}
	}
	svc, err := s.deps.Services.Update(ctx, req, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	if svc == nil {
		opErr = grpcstatus.Error(codes.NotFound, "service not found")
		return nil, opErr
	}
	return &servicev1.UpdateServiceResponse{Service: svc}, nil
}

func (s *Server) DeleteService(ctx context.Context, req *servicev1.DeleteServiceRequest) (*servicev1.DeleteServiceResponse, error) {
	serviceID := strings.TrimSpace(req.GetServiceID())
	ctx, op := publicOps.Service(ctx, ctrlobs.SpanServiceDelete, publicActionDelete, withServiceID(serviceID))
	var opErr error
	defer func() { op.End(opErr) }()
	svc, ok, err := s.deps.Services.Delete(ctx, servicekernel.DeleteParams{
		ServiceID: serviceID, ExpectedVersion: req.GetExpectedVersion(), RequireSuspended: req.GetRequireSuspended(), VolumeDisposition: req.GetVolumeDisposition(),
	}, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	if !ok {
		opErr = grpcstatus.Error(codes.NotFound, "service not found")
		return nil, opErr
	}
	return &servicev1.DeleteServiceResponse{Service: svc}, nil
}
