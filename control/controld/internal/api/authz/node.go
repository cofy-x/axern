package authz

import (
	"context"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (i *Interceptor) node(ctx context.Context) (string, time.Time, error) {
	identity, deadline, err := workloadtls.PeerIdentity(ctx, i.cluster, time.Now())
	if err != nil || identity.Role != "axnoded" {
		return "", time.Time{}, status.Error(codes.Unauthenticated, "verified Node URI identity is required")
	}
	if i.requireActiveNode == nil {
		return "", time.Time{}, status.Error(codes.Unavailable, "Node admission authority is unavailable")
	}
	if err := i.requireActiveNode(ctx, identity.NodeID); err != nil {
		return "", time.Time{}, err
	}
	return identity.NodeID, deadline, nil
}

func matchNodeRequest(req any, nodeID string) error {
	request, ok := req.(interface{ GetNodeID() string })
	if !ok || request.GetNodeID() != nodeID {
		return status.Error(codes.PermissionDenied, "request does not match authenticated Node identity")
	}
	return nil
}

type nodeServerStream struct {
	grpc.ServerStream
	ctx    context.Context
	nodeID string
}

func (s *nodeServerStream) Context() context.Context { return s.ctx }
func (s *nodeServerStream) RecvMsg(req any) error {
	if err := s.ServerStream.RecvMsg(req); err != nil {
		return err
	}
	return matchNodeRequest(req, s.nodeID)
}

func (i *Interceptor) nodeStream(srv any, stream grpc.ServerStream, next grpc.StreamHandler) error {
	nodeID, deadline, err := i.node(stream.Context())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(stream.Context(), deadline)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
				err := i.requireActiveNode(checkCtx, nodeID)
				checkCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	err = next(srv, &nodeServerStream{ServerStream: stream, ctx: ctx, nodeID: nodeID})
	cancel()
	<-done
	return err
}
