package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sync"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	nodenetworkv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/network/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type daemon struct {
	nodeID  string
	node    nodev1.NodeControlClient
	network nodenetworkv1.AllocationNetworkClient
	runsc   runscConfig
	relay   relayConfig
	mu      sync.Mutex
	running map[string]context.CancelFunc
}

type runscConfig struct {
	binary        string
	root          string
	ignoreCgroups bool
	agentBinary   string
}

type relayConfig struct {
	caCert string
}

const (
	sessionRetryMinDelay = time.Second
	sessionRetryMaxDelay = 30 * time.Second
)

func (d *daemon) run(ctx context.Context) error {
	var revision int64
	backoff := time.Second
	for {
		nextRevision, err := d.watch(ctx, revision)
		if nextRevision > revision {
			revision = nextRevision
			backoff = time.Second
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "node-tunneld: session watch failed: %v\n", err)
			if terminalControlError(err) {
				d.stopAll()
				return err
			}
		}
		if err == nil {
			continue
		}
		wait := backoff + time.Duration(rand.Int63n(int64(backoff/2+time.Millisecond)))
		if backoff < 30*time.Second {
			backoff *= 2
		}
		select {
		case <-ctx.Done():
			d.stopAll()
			return nil
		case <-time.After(wait):
		}
	}
}

func (d *daemon) watch(ctx context.Context, revision int64) (int64, error) {
	stream, err := d.node.WatchTunnelSessions(ctx, &nodev1.WatchTunnelSessionsRequest{NodeID: d.nodeID, AfterRevision: revision})
	if err != nil {
		return revision, err
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return revision, io.ErrUnexpectedEOF
		}
		if err != nil {
			return revision, err
		}
		if resp == nil {
			continue
		}
		// Never apply an old create before checking stream order: it could
		// resurrect a listener whose terminal observation was already applied.
		if resp.GetCurrentRevision() <= revision {
			return revision, fmt.Errorf("control plane returned non-advancing tunnel revision %d after %d", resp.GetCurrentRevision(), revision)
		}
		for _, item := range resp.GetSessions() {
			if item.GetSession() != nil {
				if terminal(item.GetSession().GetStatus()) {
					d.stopSession(item.GetSession().GetSessionID())
					continue
				}
				d.ensure(ctx, item)
			}
		}
		revision = resp.GetCurrentRevision()
	}
}

func terminalControlError(err error) bool {
	switch grpcstatus.Code(err) {
	case codes.NotFound, codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition:
		return true
	default:
		return false
	}
}

func (d *daemon) ensure(parent context.Context, item *nodev1.NodeTunnelSession) {
	session := item.GetSession()
	d.mu.Lock()
	if _, ok := d.running[session.GetSessionID()]; ok {
		d.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	d.running[session.GetSessionID()] = cancel
	d.mu.Unlock()
	go func() {
		defer func() {
			d.mu.Lock()
			delete(d.running, session.GetSessionID())
			d.mu.Unlock()
		}()
		d.runSession(ctx, session, item.GetNodeToken(), item.GetNodeEdgeTarget())
	}()
}

func (d *daemon) runSession(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token, nodeEdgeTarget string) {
	delay := sessionRetryMinDelay
	for {
		err := d.serveSession(ctx, session, token, nodeEdgeTarget)
		if err == nil || ctx.Err() != nil {
			return
		}
		status := statusForSessionError(err)
		_, reportErr := d.node.ReportTunnelSessionStatus(ctx, &nodev1.ReportTunnelSessionStatusRequest{
			NodeID:    d.nodeID,
			SessionID: session.GetSessionID(),
			Status:    status,
			Reason:    err.Error(),
		})
		if terminalControlError(reportErr) {
			return
		}
		if status == tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED {
			if reportErr == nil {
				return
			}
			// Keep the authoritative desired state locally until the terminal
			// observation reaches controld. A transient reporting failure must
			// not silently orphan an active control-plane session.
		} else if reportErr != nil {
			fmt.Fprintf(os.Stderr, "node-tunneld: report degraded session=%s: %v\n", session.GetSessionID(), reportErr)
		}
		wait := delay + time.Duration(rand.Int63n(int64(delay/2+time.Millisecond)))
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < sessionRetryMaxDelay {
			delay *= 2
			if delay > sessionRetryMaxDelay {
				delay = sessionRetryMaxDelay
			}
		}
	}
}

func (d *daemon) stopAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, cancel := range d.running {
		cancel()
	}
}

func (d *daemon) stopSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cancel := d.running[sessionID]; cancel != nil {
		cancel()
		delete(d.running, sessionID)
	}
}

func terminal(status tunnelcontrolv1.TunnelSessionStatus) bool {
	switch status {
	case tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED,
		tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED,
		tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED:
		return true
	default:
		return false
	}
}
