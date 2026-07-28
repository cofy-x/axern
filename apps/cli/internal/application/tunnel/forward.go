package tunnel

import (
	"context"
	"fmt"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

type ForwardParams struct {
	CreateContext         context.Context
	AllocationID          string
	RemotePort            *int32
	LocalTarget           string
	TTL                   time.Duration
	WaitReady             bool
	ReadyTimeout          time.Duration
	Relay                 RelayDialConfig
	Connector             ConnectorConfig
	DisableRenew          bool
	ConnectorRunner       ConnectorRunner
	RelayDialer           RelayPeerDialer
	OnReconnect           ConnectorReconnectReporter
	OnSessionCreated      func(ForwardSession) error
	OnConnectorStart      func(ForwardSession) error
	ConnectorReadyTimeout time.Duration
}

type ConnectorRunner func(context.Context, *tunnelcontrolv1.TunnelSession, string, string, RelayDialConfig, ConnectorConfig) error

type ForwardSession struct {
	Session     *tunnelcontrolv1.TunnelSession
	ClientToken string
}

func (c Control) Forward(ctx context.Context, params ForwardParams) error {
	createCtx := params.CreateContext
	if createCtx == nil {
		createCtx = ctx
	}
	resp, err := c.Create(createCtx, CreateParams{
		AllocationID: params.AllocationID,
		RemotePort:   params.RemotePort,
		LocalTarget:  params.LocalTarget,
		TTL:          params.TTL,
		WaitReady:    params.WaitReady,
		ReadyTimeout: params.ReadyTimeout,
	})
	if err != nil {
		return err
	}
	tunnelSession := resp.GetSession()
	if tunnelSession == nil {
		return fmt.Errorf("control plane returned empty tunnel session")
	}
	defer func() {
		revokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.revokeIfActive(revokeCtx, tunnelSession.GetSessionID(), "client disconnected")
	}()

	if params.OnSessionCreated != nil {
		if err := params.OnSessionCreated(ForwardSession{Session: tunnelSession, ClientToken: resp.GetClientToken()}); err != nil {
			return err
		}
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	var renewDone <-chan error
	if !params.DisableRenew {
		renewDone = startRenewLoop(runCtx, renewLoopConfig{
			client:      c.client,
			sessionID:   tunnelSession.GetSessionID(),
			clientToken: resp.GetClientToken(),
			ttl:         sessionLeaseTTL(tunnelSession, params.TTL),
		})
	}
	defer func() {
		cancelRun()
		if renewDone != nil {
			<-renewDone
		}
	}()
	connectorDone := make(chan error, 1)
	connectorRunner := params.ConnectorRunner
	if connectorRunner == nil {
		connectorRunner = func(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token, localTarget string, cfg RelayDialConfig, connectorCfg ConnectorConfig) error {
			return runConnector(ctx, session, token, localTarget, cfg, connectorCfg, params.RelayDialer, params.OnReconnect)
		}
	}
	go func() {
		connectorDone <- connectorRunner(runCtx, tunnelSession, resp.GetClientToken(), params.LocalTarget, params.Relay, params.Connector)
	}()
	if params.OnConnectorStart != nil {
		connectorExited, err := c.waitClientPeerConnectedOrConnectorExit(runCtx, tunnelSession.GetSessionID(), connectorDone, params.ConnectorReadyTimeout)
		if err != nil {
			cancelRun()
			if !connectorExited {
				<-connectorDone
			}
			return err
		}
		if err := params.OnConnectorStart(ForwardSession{Session: tunnelSession, ClientToken: resp.GetClientToken()}); err != nil {
			cancelRun()
			<-connectorDone
			return err
		}
		cancelRun()
		<-connectorDone
		return nil
	}
	if renewDone == nil {
		return <-connectorDone
	}
	select {
	case err := <-connectorDone:
		return err
	case err := <-renewDone:
		cancelRun()
		if err != nil {
			<-connectorDone
			return err
		}
		return nil
	}
}

func (c Control) waitClientPeerConnectedOrConnectorExit(ctx context.Context, sessionID string, connectorDone <-chan error, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		connected, err := c.clientPeerConnected(waitCtx, sessionID)
		if err == nil && connected {
			return false, nil
		}
		select {
		case err := <-connectorDone:
			if err == nil {
				return true, fmt.Errorf("tunnel connector exited before client peer connected")
			}
			return true, err
		case <-waitCtx.Done():
			return false, fmt.Errorf("tunnel client peer did not connect within %s; check relay TLS flags and relay reachability", timeout)
		case <-ticker.C:
		}
	}
}

func (c Control) clientPeerConnected(ctx context.Context, sessionID string) (bool, error) {
	resp, err := c.client.ListTunnelSessionEvents(ctx, &tunnelcontrolv1.ListTunnelSessionEventsRequest{
		SessionID: sessionID,
		Limit:     50,
	})
	if err != nil {
		return false, err
	}
	for _, event := range resp.GetEvents() {
		if event.GetEventType() == tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED {
			return true, nil
		}
	}
	return false, nil
}

func (c Control) revokeIfActive(ctx context.Context, sessionID, reason string) error {
	resp, err := c.Get(ctx, sessionID)
	if err != nil {
		_, revokeErr := c.Revoke(ctx, sessionID, reason)
		return revokeErr
	}
	if resp.GetSession() == nil {
		_, err := c.Revoke(ctx, sessionID, reason)
		return err
	}
	switch resp.GetSession().GetStatus() {
	case tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED,
		tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED,
		tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED:
		return nil
	default:
		_, err := c.Revoke(ctx, sessionID, reason)
		return err
	}
}
