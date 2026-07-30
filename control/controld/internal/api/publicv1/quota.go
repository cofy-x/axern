package publicv1

import (
	"context"
	"strings"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) GetNamespaceQuota(ctx context.Context, req *quotav1.GetNamespaceQuotaRequest) (*quotav1.GetNamespaceQuotaResponse, error) {
	namespace := strings.TrimSpace(req.GetNamespace())
	ctx, op := publicOps.Quota(ctx, ctrlobs.SpanQuotaGet, publicActionGet, withNamespace(namespace))
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Quotas == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "namespace quota control is not configured")
		return nil, opErr
	}
	if namespace == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "namespace is required")
		return nil, opErr
	}
	quota, err := s.deps.Quotas.Get(ctx, namespace)
	if err != nil {
		opErr = err
		return nil, err
	}
	return &quotav1.GetNamespaceQuotaResponse{Quota: quota}, nil
}

func (s *Server) ListNamespaceQuotas(ctx context.Context, req *quotav1.ListNamespaceQuotasRequest) (*quotav1.ListNamespaceQuotasResponse, error) {
	_ = req
	ctx, op := publicOps.Quota(ctx, ctrlobs.SpanQuotaList, publicActionList)
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Quotas == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "namespace quota control is not configured")
		return nil, opErr
	}
	actor, ok := accesskernel.ActorFromContext(ctx)
	if !ok {
		opErr = grpcstatus.Error(codes.Unauthenticated, "authenticated principal is required")
		return nil, opErr
	}
	quotas, err := s.deps.Quotas.List(ctx)
	if err != nil {
		opErr = err
		return nil, err
	}
	filtered := quotas[:0]
	for _, quota := range quotas {
		if quota != nil && accesskernel.CanReadNamespace(actor, quota.GetNamespace()) {
			filtered = append(filtered, quota)
		}
	}
	quotas = filtered
	return &quotav1.ListNamespaceQuotasResponse{Quotas: quotas}, nil
}

func (s *Server) ListNamespaceQuotaEvents(ctx context.Context, req *quotav1.ListNamespaceQuotaEventsRequest) (*quotav1.ListNamespaceQuotaEventsResponse, error) {
	namespace := strings.TrimSpace(req.GetNamespace())
	ctx, op := publicOps.Quota(ctx, ctrlobs.SpanQuotaList, publicActionList, withNamespace(namespace))
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Quotas == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "namespace quota control is not configured")
		return nil, opErr
	}
	if namespace == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "namespace is required")
		return nil, opErr
	}
	if req.GetLimit() < 0 {
		opErr = grpcstatus.Error(codes.InvalidArgument, "limit must be non-negative")
		return nil, opErr
	}
	events, err := s.deps.Quotas.ListEvents(ctx, namespace, int(req.GetLimit()))
	if err != nil {
		opErr = err
		return nil, err
	}
	return &quotav1.ListNamespaceQuotaEventsResponse{Events: events}, nil
}

func (s *Server) SetNamespaceQuota(ctx context.Context, req *quotav1.SetNamespaceQuotaRequest) (*quotav1.SetNamespaceQuotaResponse, error) {
	namespace := strings.TrimSpace(req.GetNamespace())
	ctx, op := publicOps.Quota(ctx, ctrlobs.SpanQuotaSet, publicActionSet, withNamespace(namespace))
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Quotas == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "namespace quota control is not configured")
		return nil, opErr
	}
	if namespace == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "namespace is required")
		return nil, opErr
	}
	if err := validateNamespaceQuotaLimits(req.GetLimits()); err != nil {
		opErr = err
		return nil, err
	}
	quota, err := s.deps.Quotas.Set(ctx, namespace, req.GetLimits(), s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &quotav1.SetNamespaceQuotaResponse{Quota: quota}, nil
}

func (s *Server) UnsetNamespaceQuota(ctx context.Context, req *quotav1.UnsetNamespaceQuotaRequest) (*quotav1.UnsetNamespaceQuotaResponse, error) {
	namespace := strings.TrimSpace(req.GetNamespace())
	ctx, op := publicOps.Quota(ctx, ctrlobs.SpanQuotaUnset, publicActionUnset, withNamespace(namespace))
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Quotas == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "namespace quota control is not configured")
		return nil, opErr
	}
	if namespace == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "namespace is required")
		return nil, opErr
	}
	quota, err := s.deps.Quotas.Unset(ctx, namespace, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &quotav1.UnsetNamespaceQuotaResponse{Quota: quota}, nil
}

func validateNamespaceQuotaLimits(limits *quotav1.NamespaceQuotaLimits) error {
	if limits == nil {
		return grpcstatus.Error(codes.InvalidArgument, "limits is required")
	}
	if limits.GetCpuMilli() != nil && limits.GetCpuMilli().GetValue() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "limits.cpu_milli must be >= 0")
	}
	if limits.GetMemoryBytes() != nil && limits.GetMemoryBytes().GetValue() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "limits.memory_bytes must be >= 0")
	}
	return nil
}
