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
	return s.withResolvedClient(ctx, req, gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, nodekernel.IsAllocationAccessGrantRejected, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
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
	for attempt := 1; attempt <= s.options.AccessGrantRetryAttempts; attempt++ {
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
			return grpcstatus.Error(codes.Internal, "resolved allocation access grant token is empty")
		}
		client, dialErr := s.dialer.NodeSandbox(ctx, resolved.GetNodeTarget())
		if dialErr != nil {
			return dialErr
		}
		err = call(nodekernel.WithAllocationAccessGrant(ctx, token), client)
		if attempt == s.options.AccessGrantRetryAttempts || !shouldRetry(err) {
			return unwrapAccessGrantOpenRejection(err)
		}
		if err := nodekernel.WaitAccessGrantRetry(ctx, attempt, s.options.AccessGrantRetryDelay); err != nil {
			return err
		}
		if s.metrics != nil {
			s.metrics.AccessGrantRetry("node_sandbox")
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

type accessGrantOpenRejection struct {
	err error
}

func (e accessGrantOpenRejection) Error() string { return e.err.Error() }
func (e accessGrantOpenRejection) Unwrap() error { return e.err }

func markAccessGrantOpenRejection(err error) error {
	if nodekernel.IsAllocationAccessGrantRejected(err) {
		return accessGrantOpenRejection{err: err}
	}
	return err
}

func isAccessGrantOpenRejection(err error) bool {
	var marked accessGrantOpenRejection
	return errors.As(err, &marked)
}

func unwrapAccessGrantOpenRejection(err error) error {
	var marked accessGrantOpenRejection
	if errors.As(err, &marked) {
		return marked.err
	}
	return err
}

type accessGrantHeaderClient interface {
	Header() (metadata.MD, error)
}

func acceptedAllocationAccessGrantHeader(up accessGrantHeaderClient, operation string, surfaceStatus func() error) (metadata.MD, error) {
	header, err := up.Header()
	if err != nil {
		return nil, markAccessGrantOpenRejection(err)
	}
	if !nodekernel.AllocationAccessGrantAccepted(header) {
		if err := surfaceStatus(); err != nil {
			return nil, markAccessGrantOpenRejection(err)
		}
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "node did not acknowledge allocation access grant before %s", operation)
	}
	return header, nil
}
