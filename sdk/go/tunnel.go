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
	"google.golang.org/protobuf/proto"
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
	sandbox   *Sandbox
	client    *Client
	session   *tunnelcontrolv1.TunnelSession
	ttl       time.Duration
	cancel    context.CancelFunc
	done      chan struct{}
	closeGate chan struct{}
	closed    bool
	mu        sync.Mutex
}

// OpenTunnel exposes a local upstream to the sandbox.
func (s *Sandbox) OpenTunnel(ctx context.Context, options TunnelOptions) (*SandboxTunnel, error) {
	state, err := s.State()
	if err != nil {
		return nil, err
	}
	if err := validateTunnelOptions(options); err != nil {
		return nil, err
	}
	localCtx, cancel := context.WithCancel(context.Background())
	tunnel := &SandboxTunnel{
		sandbox:   s,
		client:    s.client,
		ttl:       defaultDuration(options.TTL, defaultTunnelTTL),
		cancel:    cancel,
		done:      make(chan struct{}),
		closeGate: make(chan struct{}, 1),
	}
	if !s.registerTunnel(state.AllocationID, tunnel) {
		cancel()
		return nil, ErrSandboxNotStarted
	}
	// Closing the Sandbox also interrupts creation or peer discovery in progress.
	setupCtx, stopSetup := context.WithCancel(ctx)
	stopOnClose := context.AfterFunc(localCtx, stopSetup)
	defer stopSetup()
	defer stopOnClose()
	err = tunnel.connect(setupCtx, localCtx, state.AllocationID, options)
	if err == nil && !s.publishTunnel(state.AllocationID, tunnel) {
		err = ErrSandboxNotStarted
	}
	if err != nil {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		return nil, errors.Join(err, tunnel.Close(cleanupCtx))
	}
	return tunnel, nil
}

func (t *SandboxTunnel) connect(ctx, localCtx context.Context, allocationID string, options TunnelOptions) error {
	readyTimeout := defaultDuration(options.ReadyTimeout, defaultTunnelReadyTimeout)
	proxyPort := options.ProxyPort
	if proxyPort == 0 {
		proxyPort = defaultTunnelProxyPort
	}
	result, err := t.client.CreateTunnelSession(ctx, CreateTunnelSessionOptions{
		AllocationID: allocationID,
		RemotePort:   proxyPort,
		TTL:          t.ttl,
		WaitReady:    true,
		ReadyTimeout: readyTimeout,
	})
	if err != nil {
		close(t.done)
		return err
	}
	sessionID := result.Session.GetSessionID()
	if sessionID == "" {
		close(t.done)
		return errors.New("create tunnel session returned no session ID")
	}
	t.mu.Lock()
	t.session = result.Session
	t.mu.Unlock()

	connectorErr := make(chan error, 1)
	var wg sync.WaitGroup
	relay := t.client.relayOptions
	wg.Go(func() {
		err := tunnelConnectorRunner(localCtx, tunnelrelay.ConnectorConfig{
			SessionID:        sessionID,
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
	})
	wg.Go(func() { t.renewLoop(localCtx, sessionID, result.ClientToken) })
	go func() {
		wg.Wait()
		close(t.done)
	}()

	if err := t.waitClientPeer(ctx, sessionID, readyTimeout, connectorErr); err != nil {
		return err
	}
	session, err := t.client.GetTunnelSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.GetSessionID() != sessionID || session.GetBoundAddr() == "" {
		return fmt.Errorf("tunnel session %s returned no matching bound address", sessionID)
	}
	t.mu.Lock()
	t.session = session
	t.mu.Unlock()
	return nil
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
	return proto.CloneOf(t.session)
}

// Close revokes the tunnel session and stops SDK-owned renewal/relay work.
func (t *SandboxTunnel) Close(ctx context.Context) error {
	if t == nil {
		return nil
	}
	t.cancel()
	t.sandbox.retireTunnelAccess(t.SessionID())
	if err := acquireLifecycle(ctx, t.closeGate); err != nil {
		return err
	}
	defer func() { <-t.closeGate }()
	if t.closed {
		return nil
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if id := t.SessionID(); id != "" {
		if _, err := t.client.RevokeTunnelSession(ctx, id, "sdk close"); err != nil && !IsNotFound(err) {
			return err
		}
	}
	t.closed = true
	t.sandbox.unregisterTunnel(t)
	return nil
}

func (t *SandboxTunnel) renewLoop(ctx context.Context, sessionID, clientToken string) {
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
			session, err := t.client.RenewTunnelSession(renewCtx, sessionID, clientToken, t.ttl)
			cancel()
			if err != nil {
				if stopTunnelRenewal(err) {
					return
				}
			} else if session.GetSessionID() == sessionID {
				t.mu.Lock()
				t.session = session
				t.mu.Unlock()
			}
			timer.Reset(interval)
		}
	}
}

func (t *SandboxTunnel) waitClientPeer(ctx context.Context, sessionID string, timeout time.Duration, connectorErr <-chan error) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, err := t.client.ListTunnelSessionEvents(ctx, sessionID, defaultTunnelEventLimit)
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
			if !ok {
				return errors.New("tunnel connector stopped before its peer connected")
			}
			return err
		case <-ticker.C:
		}
	}
}

func stopTunnelRenewal(err error) bool {
	code := errorCode(err)
	return code == codes.NotFound || code == codes.FailedPrecondition || code == codes.PermissionDenied || code == codes.Unauthenticated
}

func defaultInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
