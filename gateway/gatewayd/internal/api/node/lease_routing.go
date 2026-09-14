package node

import (
	"context"
	"errors"
	"strings"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (s *Server) unary(ctx context.Context, req proto.Message, call func(context.Context, nodesandboxv1.NodeSandboxClient) error) error {
	return s.withResolvedClient(ctx, req, gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, nodekernel.IsExecutionLeaseRejected, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		return call(backendCtx, client)
	})
}

func serverStream[T any](s *Server, ctx context.Context, req proto.Message, shouldRetry func(error) bool, open func(context.Context, nodesandboxv1.NodeSandboxClient) (T, error), call func(T) error) error {
	return serverStreamForPurpose(s, ctx, req, gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, shouldRetry, open, call)
}

func serverStreamForPurpose[T any](s *Server, ctx context.Context, req proto.Message, purpose gatewayv1.AllocationAccessPurpose, shouldRetry func(error) bool, open func(context.Context, nodesandboxv1.NodeSandboxClient) (T, error), call func(T) error) error {
	return s.withResolvedClient(ctx, req, purpose, shouldRetry, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		up, err := open(backendCtx, client)
		if err != nil {
			return err
		}
		return call(up)
	})
}

func bidi[T interface{ CloseSend() error }](s *Server, ctx context.Context, req proto.Message, shouldRetry func(error) bool, open func(context.Context, nodesandboxv1.NodeSandboxClient) (T, error), call func(T) error) error {
	return s.withResolvedClient(ctx, req, gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, shouldRetry, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		up, err := open(backendCtx, client)
		if err != nil {
			return err
		}
		defer up.CloseSend()
		return call(up)
	})
}

func (s *Server) withResolvedClient(ctx context.Context, req proto.Message, purpose gatewayv1.AllocationAccessPurpose, shouldRetry func(error) bool, call func(context.Context, nodesandboxv1.NodeSandboxClient) error) error {
	if req == nil {
		return grpcstatus.Error(codes.InvalidArgument, "request is required")
	}
	allocationID := allocationID(req)
	if allocationID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	fingerprint, authErr := s.options.ClientFingerprint(ctx)
	if authErr != nil {
		return authErr
	}
	var err error
	for attempt := 1; attempt <= s.options.LeaseRetryAttempts; attempt++ {
		resolved, resolveErr := s.resolver.ResolveAllocationTerminal(ctx, &gatewayv1.ResolveAllocationTerminalRequest{
			AllocationID:                 allocationID,
			TtlSeconds:                   300,
			ClientCertificateFingerprint: fingerprint,
			Purpose:                      purpose,
		})
		if resolveErr != nil {
			return resolveErr
		}
		if strings.TrimSpace(resolved.GetAllocationID()) != allocationID {
			return grpcstatus.Error(codes.Internal, "resolved allocation identity does not match request")
		}
		token := strings.TrimSpace(resolved.GetAccessGrant().GetPlaintextToken())
		if token == "" {
			return grpcstatus.Error(codes.Internal, "resolved execution lease token is empty")
		}
		client, dialErr := s.dialer.NodeSandbox(ctx, resolved.GetNodeTarget())
		if dialErr != nil {
			return dialErr
		}
		err = call(nodekernel.WithExecutionLease(ctx, token), client)
		if attempt == s.options.LeaseRetryAttempts || !shouldRetry(err) {
			return unwrapLeaseOpenRejection(err)
		}
		if err := nodekernel.WaitLeaseRetry(ctx, attempt, s.options.LeaseRetryDelay); err != nil {
			return err
		}
		if s.metrics != nil {
			s.metrics.LeaseRetry("node_sandbox")
		}
	}
	return err
}

func allocationID(msg proto.Message) string {
	field := msg.ProtoReflect().Descriptor().Fields().ByName("allocation_id")
	if field == nil {
		return ""
	}
	return strings.TrimSpace(msg.ProtoReflect().Get(field).String())
}

type leaseOpenRejection struct {
	err error
}

func (e leaseOpenRejection) Error() string { return e.err.Error() }
func (e leaseOpenRejection) Unwrap() error { return e.err }

func markLeaseOpenRejection(err error) error {
	if nodekernel.IsExecutionLeaseRejected(err) {
		return leaseOpenRejection{err: err}
	}
	return err
}

func isLeaseOpenRejection(err error) bool {
	var marked leaseOpenRejection
	return errors.As(err, &marked)
}

func unwrapLeaseOpenRejection(err error) error {
	var marked leaseOpenRejection
	if errors.As(err, &marked) {
		return marked.err
	}
	return err
}

type executionLeaseHeaderClient interface {
	Header() (metadata.MD, error)
}

func acceptedExecutionLeaseHeader(up executionLeaseHeaderClient, operation string, surfaceStatus func() error) (metadata.MD, error) {
	header, err := up.Header()
	if err != nil {
		return nil, markLeaseOpenRejection(err)
	}
	if !nodekernel.ExecutionLeaseAccepted(header) {
		if err := surfaceStatus(); err != nil {
			return nil, markLeaseOpenRejection(err)
		}
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "node did not acknowledge execution lease before %s", operation)
	}
	return header, nil
}
