package relay

import (
	"context"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) readLoop(ctx context.Context, stream tunnelv1.TunnelRelay_ConnectPeerServer, sessionID string, p *peer) error {
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		p.lastSeen.Store(time.Now())
		if s.frameBytes(frame) > s.maxFrameBytes {
			return grpcstatus.Error(codes.ResourceExhausted, "tunnel frame too large")
		}
		if frame.GetPeerOpen() != nil {
			continue
		}
		if ping := frame.GetPing(); ping != nil {
			if err := s.enqueue(ctx, p, &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Pong{Pong: &tunnelv1.Pong{ID: ping.GetID()}}}); err != nil {
				return err
			}
			continue
		}
		if frame.GetPong() != nil {
			continue
		}
		dst := s.waitOpposite(ctx, sessionID, p)
		if dst == nil {
			return grpcstatus.Error(codes.Unavailable, "opposite tunnel peer is not connected")
		}
		if err := s.enqueue(ctx, dst, frame); err != nil {
			return err
		}
		s.recordFrameForwarded(stream.Context(), frame)
		if data := frame.GetStreamData(); data != nil {
			n := int64(len(data.GetData()))
			p.bytesOut.Add(n)
			dst.bytesIn.Add(n)
		}
	}
}

func (s *Server) waitOpposite(ctx context.Context, sessionID string, p *peer) *peer {
	ctx, cancel := context.WithTimeout(ctx, s.pairWaitTimeout)
	defer cancel()
	for {
		dst, changed := s.oppositeState(sessionID, p)
		if dst != nil {
			return dst
		}
		select {
		case <-ctx.Done():
			return nil
		case <-p.done:
			return nil
		case <-changed:
		}
	}
}

func (s *Server) enqueue(_ context.Context, p *peer, frame *tunnelv1.TunnelFrame) error {
	select {
	case p.send <- frame:
		return nil
	case <-p.done:
		return grpcstatus.Error(codes.Unavailable, "tunnel peer disconnected")
	default:
		err := grpcstatus.Error(codes.ResourceExhausted, "tunnel peer send queue full")
		s.closePeerSessionWithError(p.sessionID, p, err)
		return err
	}
}

func (s *Server) frameBytes(frame *tunnelv1.TunnelFrame) int {
	if frame == nil {
		return 0
	}
	if data := frame.GetStreamData(); data != nil {
		return len(data.GetData())
	}
	return 0
}

func writeLoop(ctx context.Context, stream tunnelv1.TunnelRelay_ConnectPeerServer, p *peer) error {
	for {
		select {
		case frame := <-p.send:
			if frame == nil {
				continue
			}
			if err := stream.Send(frame); err != nil {
				return err
			}
		case <-p.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
