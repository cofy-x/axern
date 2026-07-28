package controlv1

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

type Session struct {
	Context context.Context
	Cancel  context.CancelFunc
	Conn    *grpc.ClientConn
	Clients Clients
}

func Open(parent context.Context, config Config) (*Session, error) {
	cmdCtx, cancel := commandContext(parent, config.Timeout)
	conn, clients, err := dial(cmdCtx, config)
	if err != nil {
		cancel()
		return nil, err
	}
	return &Session{
		Context: cmdCtx,
		Cancel:  cancel,
		Conn:    conn,
		Clients: clients,
	}, nil
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	if s.Cancel != nil {
		s.Cancel()
	}
	if s.Conn != nil {
		return s.Conn.Close()
	}
	return nil
}

func commandContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
