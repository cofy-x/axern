package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ConnectorConfig struct {
	PingInterval time.Duration
	PongTimeout  time.Duration
	MaxStreams   int
}

type RelayPeerStream interface {
	Send(*tunnelv1.TunnelFrame) error
	Recv() (*tunnelv1.TunnelFrame, error)
}

type RelayPeerDialer func(context.Context, string, RelayDialConfig) (RelayPeerStream, io.Closer, error)

type ConnectorReconnectReporter func(error, time.Duration)

func runConnector(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token, localTarget string, cfg RelayDialConfig, connectorCfg ConnectorConfig, dialer RelayPeerDialer, reportReconnect ConnectorReconnectReporter) error {
	if dialer == nil {
		return fmt.Errorf("tunnel relay dialer is required")
	}
	backoff := time.Second
	for {
		err := runConnectorOnce(ctx, session, token, localTarget, cfg, connectorCfg, dialer)
		if ctx.Err() != nil {
			return nil
		}
		if terminalConnectorError(err) {
			return err
		}
		if err != nil && reportReconnect != nil {
			reportReconnect(err, backoff)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}

func terminalConnectorError(err error) bool {
	if err == nil {
		return false
	}
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated, codes.NotFound:
		return true
	default:
		return false
	}
}

func runConnectorOnce(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token, localTarget string, cfg RelayDialConfig, connectorCfg ConnectorConfig, dialer RelayPeerDialer) error {
	target := session.GetClientEdgeTarget()
	if target == "" {
		target = session.GetEdgeTarget()
	}
	stream, closer, err := dialer(ctx, target, cfg)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{
		SessionID: session.GetSessionID(),
		PeerKind:  tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		Token:     token,
	}}}); err != nil {
		return err
	}
	c := &connector{stream: stream, localTarget: localTarget, conns: make(map[uint64]net.Conn), config: connectorCfg}
	c.lastSeen.Store(time.Now())
	return c.run(ctx)
}

type connector struct {
	stream      RelayPeerStream
	localTarget string
	writeMu     sync.Mutex
	mu          sync.Mutex
	conns       map[uint64]net.Conn
	lastSeen    atomicTime
	config      ConnectorConfig
}

func (c *connector) run(ctx context.Context) error {
	defer c.closeAll()
	errCh := make(chan error, 2)
	go func() {
		for {
			frame, err := c.stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			c.lastSeen.Store(time.Now())
			c.handleFrame(ctx, frame)
		}
	}()
	go func() { errCh <- c.heartbeatLoop(ctx) }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

func (c *connector) heartbeatLoop(ctx context.Context) error {
	if c.config.PingInterval <= 0 {
		<-ctx.Done()
		return nil
	}
	timeout := c.config.PongTimeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ticker := time.NewTicker(c.config.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if time.Since(c.lastSeen.Load()) > timeout {
				return fmt.Errorf("tunnel connector pong timeout")
			}
			if err := c.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Ping{Ping: &tunnelv1.Ping{ID: fmt.Sprintf("%d", time.Now().UnixNano())}}}); err != nil {
				return err
			}
		}
	}
}

type atomicTime struct {
	nano atomic.Int64
}

func (t *atomicTime) Store(v time.Time) {
	t.nano.Store(v.UnixNano())
}

func (t *atomicTime) Load() time.Time {
	nano := t.nano.Load()
	if nano == 0 {
		return time.Now()
	}
	return time.Unix(0, nano)
}

func (c *connector) send(frame *tunnelv1.TunnelFrame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.stream.Send(frame)
}
