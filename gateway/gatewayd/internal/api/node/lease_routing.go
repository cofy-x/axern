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
	"google.golang.org/protobuf/reflect/protoreflect"
)

func (s *Server) unary(ctx context.Context, req proto.Message, call func(nodesandboxv1.NodeSandboxClient) error) error {
	return s.withResolvedClient(ctx, req, nodekernel.IsExecutionLeaseRejected, func(client nodesandboxv1.NodeSandboxClient) error {
		return call(client)
	})
}

func serverStream[T any](s *Server, ctx context.Context, req proto.Message, shouldRetry func(error) bool, open func(nodesandboxv1.NodeSandboxClient) (T, error), call func(T) error) error {
	return s.withResolvedClient(ctx, req, shouldRetry, func(client nodesandboxv1.NodeSandboxClient) error {
		up, err := open(client)
		if err != nil {
			return err
		}
		return call(up)
	})
}

func bidi[T interface{ CloseSend() error }](s *Server, ctx context.Context, req proto.Message, shouldRetry func(error) bool, open func(nodesandboxv1.NodeSandboxClient) (T, error), call func(T) error) error {
	return s.withResolvedClient(ctx, req, shouldRetry, func(client nodesandboxv1.NodeSandboxClient) error {
		up, err := open(client)
		if err != nil {
			return err
		}
		defer up.CloseSend()
		return call(up)
	})
}

func (s *Server) withResolvedClient(ctx context.Context, req proto.Message, shouldRetry func(error) bool, call func(nodesandboxv1.NodeSandboxClient) error) error {
	if req == nil {
		return grpcstatus.Error(codes.InvalidArgument, "request is required")
	}
	allocationID := allocationID(req)
	if allocationID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	var err error
	for attempt := 1; attempt <= s.options.LeaseRetryAttempts; attempt++ {
		resolved, resolveErr := s.resolver.ResolveAllocationTerminal(ctx, &gatewayv1.ResolveAllocationTerminalRequest{
			AllocationID: allocationID,
			TtlSeconds:   300,
		})
		if resolveErr != nil {
			return resolveErr
		}
		if err = injectLease(req, resolved); err != nil {
			return err
		}
		client, dialErr := s.dialer.NodeSandbox(ctx, resolved.GetNodeTarget())
		if dialErr != nil {
			return dialErr
		}
		err = call(client)
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

func injectLease(msg proto.Message, resolved *gatewayv1.ResolveAllocationTerminalResponse) error {
	if msg == nil {
		return grpcstatus.Error(codes.InvalidArgument, "request is required")
	}
	fields := msg.ProtoReflect().Descriptor().Fields()
	setString(msg, fields, "allocation_id", resolved.GetAllocationID())
	setInt(msg, fields, "attempt", resolved.GetAttempt())
	setString(msg, fields, "execution_lease_token", resolved.GetLease().GetPlaintextToken())
	return nil
}

func setString(msg proto.Message, fields protoreflect.FieldDescriptors, name protoreflect.Name, value string) {
	field := fields.ByName(name)
	if field != nil {
		msg.ProtoReflect().Set(field, protoreflect.ValueOfString(value))
	}
}

func setInt(msg proto.Message, fields protoreflect.FieldDescriptors, name protoreflect.Name, value int64) {
	field := fields.ByName(name)
	if field != nil {
		msg.ProtoReflect().Set(field, protoreflect.ValueOfInt64(value))
	}
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
