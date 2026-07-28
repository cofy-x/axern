package relay

import (
	"hash/maphash"
	"time"
)

type Option func(*Server)

func WithPeerOpenTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		if timeout > 0 {
			s.peerOpenTimeout = timeout
		}
	}
}

func WithMaxSessions(maxSessions int) Option {
	return func(s *Server) {
		if maxSessions >= 0 {
			s.maxSessions = maxSessions
		}
	}
}

func WithPeerRevalidateInterval(interval time.Duration) Option {
	return func(s *Server) {
		if interval >= 0 {
			s.peerRevalidateInterval = interval
		}
	}
}

func WithRelayID(id string) Option {
	return func(s *Server) {
		if id != "" {
			s.relayID = id
		}
	}
}

func WithDrain(drain bool) Option {
	return func(s *Server) {
		s.drain = drain
	}
}

func WithSendQueueSize(size int) Option {
	return func(s *Server) {
		if size > 0 {
			s.sendQueueSize = size
		}
	}
}

func WithMaxFrameBytes(size int) Option {
	return func(s *Server) {
		if size > 0 {
			s.maxFrameBytes = size
		}
	}
}

func WithPairWaitTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		if timeout > 0 {
			s.pairWaitTimeout = timeout
		}
	}
}

func WithPingInterval(interval time.Duration) Option {
	return func(s *Server) {
		if interval >= 0 {
			s.pingInterval = interval
		}
	}
}

func WithPongTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		if timeout > 0 {
			s.pongTimeout = timeout
		}
	}
}

func New(control ControlClient, options ...Option) *Server {
	s := &Server{
		control:                control,
		relayID:                "default",
		peerOpenTimeout:        10 * time.Second,
		peerRevalidateInterval: 15 * time.Second,
		maxSessions:            10000,
		sendQueueSize:          128,
		maxFrameBytes:          1024 * 1024,
		pairWaitTimeout:        30 * time.Second,
		pingInterval:           15 * time.Second,
		pongTimeout:            45 * time.Second,
	}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	s.registry.init(maphash.MakeSeed(), s.maxSessions)
	return s
}
