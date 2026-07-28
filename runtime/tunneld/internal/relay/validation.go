package relay

import (
	"context"
	"fmt"
	"os"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) revalidateLoop(ctx context.Context, p *peer) error {
	if s.peerRevalidateInterval <= 0 {
		<-p.done
		return nil
	}
	ticker := time.NewTicker(s.peerRevalidateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
			return nil
		case <-ticker.C:
			if err := s.revalidatePeer(ctx, p); err != nil {
				if terminalValidationError(err) {
					fmt.Fprintf(os.Stderr, "tunneld: peer revalidation closed session=%s kind=%s err=%v\n", p.sessionID, p.kind.String(), err)
					s.closePeerSessionWithError(p.sessionID, p, err)
					return err
				}
				fmt.Fprintf(os.Stderr, "tunneld: peer revalidation transient failure session=%s kind=%s err=%v\n", p.sessionID, p.kind.String(), err)
			}
		}
	}
}

func (s *Server) revalidatePeer(ctx context.Context, p *peer) error {
	if s.control == nil {
		return grpcstatus.Error(codes.FailedPrecondition, "control validator is not configured")
	}
	_, err := s.control.ValidateTunnelPeer(ctx, &tunnelcontrolv1.ValidateTunnelPeerRequest{
		SessionID: p.sessionID,
		PeerKind:  p.kind,
		Token:     p.token,
	})
	return err
}

func terminalValidationError(err error) bool {
	switch grpcstatus.Code(err) {
	case codes.PermissionDenied, codes.NotFound, codes.FailedPrecondition, codes.Unauthenticated:
		return true
	default:
		return false
	}
}

func recvInitialFrame(stream tunnelv1.TunnelRelay_ConnectPeerServer, timeout time.Duration) (*tunnelv1.TunnelFrame, error) {
	if timeout <= 0 {
		return stream.Recv()
	}
	type result struct {
		frame *tunnelv1.TunnelFrame
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		frame, err := stream.Recv()
		ch <- result{frame: frame, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res.frame, res.err
	case <-timer.C:
		return nil, grpcstatus.Error(codes.DeadlineExceeded, "initial peer_open frame timed out")
	case <-stream.Context().Done():
		return nil, stream.Context().Err()
	}
}
