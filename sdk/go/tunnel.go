package axernsdk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/cofy-x/axern/sdk/go/internal/tunnelrelay"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultTunnelProxyPort    int32 = 8786
	defaultTunnelTTL                = 5 * time.Minute
	defaultTunnelReadyTimeout       = 30 * time.Second
	defaultTunnelRenewTimeout       = 10 * time.Second
	defaultTunnelPingInterval       = 15 * time.Second
	defaultTunnelDialTimeout        = 5 * time.Second
	defaultTunnelMaxStreams         = 128
	defaultTunnelEventLimit   int32 = 30
)

var tunnelConnectorRunner = tunnelrelay.RunConnector

// TunnelOptions configures a sandbox tunnel to a local upstream.
type TunnelOptions struct {
	Upstream     string
	ProxyPort    int32
	TTL          time.Duration
	ReadyTimeout time.Duration
	Connector    TunnelConnectorOptions
}

// TunnelConnectorOptions configures SDK-owned relay connector behavior.
type TunnelConnectorOptions struct {
	PingInterval time.Duration
	DialTimeout  time.Duration
	MaxStreams   int
}

// SandboxTunnel is an SDK-owned tunnel session with renewal and cleanup.
type SandboxTunnel struct {
	sandbox     *Sandbox
	client      *Client
	session     *tunnelcontrolv1.TunnelSession
	clientToken string
	ttl         time.Duration
	cancel      context.CancelFunc
	done        chan struct{}
	closeOnce   sync.Once
	closeErr    error
	mu          sync.Mutex
}

// OpenTunnel exposes a local upstream to the sandbox.
func (s *Sandbox) OpenTunnel(ctx context.Context, options TunnelOptions) (*SandboxTunnel, error) {
	if !s.started {
		return nil, ErrSandboxNotStarted
	}
	if err := validateTunnelOptions(options); err != nil {
		return nil, err
	}
	ttl := defaultDuration(options.TTL, defaultTunnelTTL)
	readyTimeout := defaultDuration(options.ReadyTimeout, defaultTunnelReadyTimeout)
	proxyPort := options.ProxyPort
	if proxyPort == 0 {
		proxyPort = defaultTunnelProxyPort
	}
	result, err := s.client.CreateTunnelSession(ctx, CreateTunnelSessionOptions{
		AllocationID: s.state.AllocationID,
		LocalTarget:  options.Upstream,
		RemotePort:   proxyPort,
		TTL:          ttl,
		WaitReady:    true,
		ReadyTimeout: readyTimeout,
	})
	if err != nil {
		return nil, err
	}
	if result.Session == nil {
		return nil, fmt.Errorf("create tunnel session returned no session")
	}
	tunnelCtx, cancel := context.WithCancel(context.Background())
	tunnel := &SandboxTunnel{
		sandbox:     s,
		client:      s.client,
		session:     result.Session,
		clientToken: result.ClientToken,
		ttl:         ttl,
		cancel:      cancel,
		done:        make(chan struct{}),
	}

	connectorErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	relay := s.client.relayOptions
	go func() {
		defer wg.Done()
		err := tunnelConnectorRunner(tunnelCtx, tunnelrelay.ConnectorConfig{
			SessionID:        result.Session.GetSessionID(),
			EdgeTarget:       result.Session.GetEdgeTarget(),
			ClientEdgeTarget: result.Session.GetClientEdgeTarget(),
			ClientToken:      result.ClientToken,
			LocalTarget:      options.Upstream,
			RelayInsecure:    relay.Insecure,
			RelayTLSCACert:   relay.TLSCACert,
			RelayTLSCert:     relay.TLSCert,
			RelayTLSKey:      relay.TLSKey,
			RelayServerName:  relay.ServerName,
			ProxyMode:        relay.ProxyMode,
			PingInterval:     defaultDuration(options.Connector.PingInterval, defaultTunnelPingInterval),
			DialTimeout:      defaultDuration(options.Connector.DialTimeout, defaultTunnelDialTimeout),
			MaxStreams:       defaultInt(options.Connector.MaxStreams, defaultTunnelMaxStreams),
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			connectorErr <- err
		}
		close(connectorErr)
	}()
	go func() {
		defer wg.Done()
		tunnel.renewLoop(tunnelCtx)
	}()
	go func() {
		wg.Wait()
		close(tunnel.done)
	}()

	if err := s.waitTunnelClientPeer(ctx, result.Session.GetSessionID(), readyTimeout, connectorErr); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = tunnel.Close(cleanupCtx)
		return nil, err
	}
	if session, err := s.client.GetTunnelSession(ctx, result.Session.GetSessionID()); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = tunnel.Close(cleanupCtx)
		return nil, err
	} else if session != nil {
		tunnel.mu.Lock()
		tunnel.session = session
		tunnel.mu.Unlock()
	}
	if tunnel.BoundAddr() == "" {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = tunnel.Close(cleanupCtx)
		return nil, fmt.Errorf("tunnel session %s has no bound address", tunnel.SessionID())
	}
	s.registerTunnel(tunnel)
	s.state.TunnelSessionID = tunnel.SessionID()
	s.state.BoundAddr = tunnel.BoundAddr()
	return tunnel, nil
}

func validateTunnelOptions(options TunnelOptions) error {
	if isBlank(options.Upstream) {
		return requiredError("upstream")
	}
	if options.ProxyPort < 0 {
		return positiveIntError("proxy_port")
	}
	if options.TTL < 0 {
		return positiveDurationError("ttl")
	}
	if options.ReadyTimeout < 0 {
		return positiveDurationError("ready_timeout")
	}
	if options.Connector.PingInterval < 0 {
		return positiveDurationError("connector.ping_interval")
	}
	if options.Connector.DialTimeout < 0 {
		return positiveDurationError("connector.dial_timeout")
	}
	if options.Connector.MaxStreams < 0 {
		return positiveIntError("connector.max_streams")
	}
	return nil
}

// SessionID returns the control-plane tunnel session id.
func (t *SandboxTunnel) SessionID() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session == nil {
		return ""
	}
	return t.session.GetSessionID()
}

// BoundAddr returns the sandbox-local address bound by the tunnel.
func (t *SandboxTunnel) BoundAddr() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session == nil {
		return ""
	}
	return t.session.GetBoundAddr()
}

// Session returns the latest control-plane tunnel session payload.
func (t *SandboxTunnel) Session() *tunnelcontrolv1.TunnelSession {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.session
}

// Close revokes the tunnel session and stops SDK-owned renewal/relay work.
func (t *SandboxTunnel) Close(ctx context.Context) error {
	if t == nil {
		return nil
	}
	t.closeOnce.Do(func() {
		t.cancel()
		select {
		case <-t.done:
		case <-ctx.Done():
			t.closeErr = ctx.Err()
			return
		}
		if t.sandbox != nil {
			t.sandbox.unregisterTunnel(t)
		}
		if t.SessionID() == "" {
			return
		}
		if _, err := t.client.RevokeTunnelSession(ctx, t.SessionID(), "sdk close"); err != nil && !isTerminalTunnelCloseError(err) {
			t.closeErr = err
		}
	})
	return t.closeErr
}

func (t *SandboxTunnel) renewLoop(ctx context.Context) {
	interval := t.ttl / 2
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			renewCtx, cancel := context.WithTimeout(ctx, defaultTunnelRenewTimeout)
			session, err := t.client.RenewTunnelSession(renewCtx, t.SessionID(), t.clientToken, t.ttl)
			cancel()
			if err != nil {
				if isTerminalTunnelCloseError(err) {
					return
				}
			} else if session != nil {
				t.mu.Lock()
				t.session = session
				t.mu.Unlock()
			}
			timer.Reset(interval)
		}
	}
}

func (s *Sandbox) waitTunnelClientPeer(ctx context.Context, sessionID string, timeout time.Duration, connectorErr <-chan error) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, err := s.client.ListTunnelSessionEvents(ctx, sessionID, defaultTunnelEventLimit)
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.GetEventType() == tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("tunnel session %s did not connect client peer within %s", sessionID, timeout)
		case err, ok := <-connectorErr:
			if ok && err != nil {
				return err
			}
		case <-ticker.C:
		}
	}
}

func isTerminalTunnelCloseError(err error) bool {
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr.Code == codes.NotFound || rpcErr.Code == codes.FailedPrecondition || rpcErr.Code == codes.PermissionDenied || rpcErr.Code == codes.Unauthenticated
	}
	code := status.Code(err)
	return code == codes.NotFound || code == codes.FailedPrecondition || code == codes.PermissionDenied || code == codes.Unauthenticated
}

func defaultInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
