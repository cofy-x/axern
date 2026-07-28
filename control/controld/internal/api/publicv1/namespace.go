package publicv1

import (
	"context"
	"strings"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) CreateNamespace(ctx context.Context, req *namespacev1.CreateNamespaceRequest) (*namespacev1.CreateNamespaceResponse, error) {
	namespace := strings.TrimSpace(req.GetNamespace())
	ctx, op := publicOps.Namespace(ctx, ctrlobs.SpanNamespaceCreate, publicActionCreate, withNamespace(namespace))
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Namespaces == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "namespace control is not configured")
		return nil, opErr
	}
	if namespace == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "namespace is required")
		return nil, opErr
	}
	record, err := s.deps.Namespaces.CreateNamespace(ctx, namespace, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &namespacev1.CreateNamespaceResponse{Namespace: record}, nil
}

func (s *Server) GetNamespace(ctx context.Context, req *namespacev1.GetNamespaceRequest) (*namespacev1.GetNamespaceResponse, error) {
	namespace := strings.TrimSpace(req.GetNamespace())
	ctx, op := publicOps.Namespace(ctx, ctrlobs.SpanNamespaceGet, publicActionGet, withNamespace(namespace))
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Namespaces == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "namespace control is not configured")
		return nil, opErr
	}
	if namespace == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "namespace is required")
		return nil, opErr
	}
	record, err := s.deps.Namespaces.GetNamespace(ctx, namespace)
	if err != nil {
		opErr = err
		return nil, err
	}
	return &namespacev1.GetNamespaceResponse{Namespace: record}, nil
}

func (s *Server) ListNamespaces(ctx context.Context, req *namespacev1.ListNamespacesRequest) (*namespacev1.ListNamespacesResponse, error) {
	_ = req
	ctx, op := publicOps.Namespace(ctx, ctrlobs.SpanNamespaceList, publicActionList)
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Namespaces == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "namespace control is not configured")
		return nil, opErr
	}
	namespaces, err := s.deps.Namespaces.ListNamespaces(ctx)
	if err != nil {
		opErr = err
		return nil, err
	}
	return &namespacev1.ListNamespacesResponse{Namespaces: namespaces}, nil
}

func (s *Server) DeleteNamespace(ctx context.Context, req *namespacev1.DeleteNamespaceRequest) (*namespacev1.DeleteNamespaceResponse, error) {
	namespace := strings.TrimSpace(req.GetNamespace())
	ctx, op := publicOps.Namespace(ctx, ctrlobs.SpanNamespaceDelete, publicActionDelete, withNamespace(namespace))
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Namespaces == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "namespace control is not configured")
		return nil, opErr
	}
	if namespace == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "namespace is required")
		return nil, opErr
	}
	record, err := s.deps.Namespaces.DeleteNamespace(ctx, namespace, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &namespacev1.DeleteNamespaceResponse{Namespace: record}, nil
}
