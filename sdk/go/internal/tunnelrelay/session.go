package tunnelrelay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

type sessionConfig struct {
	PingInterval time.Duration
	DialTimeout  time.Duration
	MaxStreams   int
}

type session struct {
	stream       tunnelv1.TunnelRelay_ConnectPeerClient
	localTarget  string
	sendMu       sync.Mutex
	streamsMu    sync.Mutex
	streams      map[uint64]*localStream
	maxStreams   int
	dialTimeout  time.Duration
	pingInterval time.Duration
}

type localStream struct {
	conn net.Conn
	once sync.Once
}

func newSession(stream tunnelv1.TunnelRelay_ConnectPeerClient, localTarget string, config sessionConfig) *session {
	return &session{
		stream:       stream,
		localTarget:  localTarget,
		streams:      map[uint64]*localStream{},
		maxStreams:   config.MaxStreams,
		dialTimeout:  config.DialTimeout,
		pingInterval: config.PingInterval,
	}
}

func (s *session) run(ctx context.Context) error {
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		s.heartbeat(ctx)
	}()
	defer func() {
		s.closeAll()
		<-pingDone
	}()
	for {
		frame, err := s.stream.Recv()
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			return err
		}
		if err := s.handleFrame(ctx, frame); err != nil {
			return err
		}
	}
}

func (s *session) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Ping{Ping: &tunnelv1.Ping{ID: fmt.Sprintf("%d", time.Now().UnixNano())}}})
		}
	}
}

func (s *session) handleFrame(ctx context.Context, frame *tunnelv1.TunnelFrame) error {
	switch payload := frame.GetPayload().(type) {
	case *tunnelv1.TunnelFrame_Ping:
		return s.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Pong{Pong: &tunnelv1.Pong{ID: payload.Ping.GetID()}}})
	case *tunnelv1.TunnelFrame_Pong:
		return nil
	case *tunnelv1.TunnelFrame_StreamOpen:
		s.openLocal(ctx, payload.StreamOpen.GetStreamID())
		return nil
	case *tunnelv1.TunnelFrame_StreamData:
		s.writeLocal(payload.StreamData.GetStreamID(), payload.StreamData.GetData())
		return nil
	case *tunnelv1.TunnelFrame_StreamClose:
		s.closeLocal(payload.StreamClose.GetStreamID())
		return nil
	default:
		return nil
	}
}

func (s *session) openLocal(ctx context.Context, streamID uint64) {
	if s.streamCount() >= s.maxStreams {
		_ = s.sendClose(streamID, "max local streams reached")
		return
	}
	dialer := net.Dialer{Timeout: s.dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.localTarget)
	if err != nil {
		_ = s.sendClose(streamID, err.Error())
		return
	}
	local := &localStream{conn: conn}
	s.streamsMu.Lock()
	s.streams[streamID] = local
	s.streamsMu.Unlock()
	go s.copyLocalToRelay(streamID, local)
}

func (s *session) writeLocal(streamID uint64, data []byte) {
	local := s.localStream(streamID)
	if local == nil {
		_ = s.sendClose(streamID, "local stream is not open")
		return
	}
	if _, err := local.conn.Write(data); err != nil {
		_ = s.sendClose(streamID, err.Error())
		s.closeLocal(streamID)
	}
}

func (s *session) copyLocalToRelay(streamID uint64, local *localStream) {
	buffer := make([]byte, 32*1024)
	for {
		n, err := local.conn.Read(buffer)
		if n > 0 {
			if sendErr := s.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{StreamData: &tunnelv1.StreamData{
				StreamID: streamID,
				Data:     append([]byte(nil), buffer[:n]...),
			}}}); sendErr != nil {
				s.closeLocal(streamID)
				return
			}
		}
		if err != nil {
			closeErr := ""
			if !errors.Is(err, io.EOF) {
				closeErr = err.Error()
			}
			_ = s.sendClose(streamID, closeErr)
			s.closeLocal(streamID)
			return
		}
	}
}

func (s *session) localStream(streamID uint64) *localStream {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	return s.streams[streamID]
}

func (s *session) streamCount() int {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	return len(s.streams)
}

func (s *session) closeLocal(streamID uint64) {
	s.streamsMu.Lock()
	local := s.streams[streamID]
	delete(s.streams, streamID)
	s.streamsMu.Unlock()
	if local != nil {
		local.once.Do(func() {
			_ = local.conn.Close()
		})
	}
}

func (s *session) closeAll() {
	s.streamsMu.Lock()
	streams := s.streams
	s.streams = map[uint64]*localStream{}
	s.streamsMu.Unlock()
	for _, local := range streams {
		local.once.Do(func() {
			_ = local.conn.Close()
		})
	}
}

func (s *session) sendClose(streamID uint64, message string) error {
	return s.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamClose{StreamClose: &tunnelv1.StreamClose{
		StreamID: streamID,
		Error:    message,
	}}})
}

func (s *session) send(frame *tunnelv1.TunnelFrame) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.stream.Send(frame)
}
