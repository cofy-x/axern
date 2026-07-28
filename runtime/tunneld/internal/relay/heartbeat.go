package relay

import (
	"context"
	"fmt"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

var errPeerPongTimeout = grpcstatus.Error(codes.DeadlineExceeded, "tunnel peer pong timeout")

func (s *Server) heartbeatLoop(ctx context.Context, p *peer) error {
	if s.pingInterval <= 0 {
		<-p.done
		return nil
	}
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
			return nil
		case <-ticker.C:
			if time.Since(p.lastSeen.Load()) > s.pongTimeout {
				s.closePeerSessionWithError(p.sessionID, p, errPeerPongTimeout)
				return errPeerPongTimeout
			}
			if err := s.enqueue(ctx, p, &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Ping{Ping: &tunnelv1.Ping{ID: fmt.Sprintf("%d", time.Now().UnixNano())}}}); err != nil {
				return err
			}
		}
	}
}
