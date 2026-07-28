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

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type daemon struct {
	nodeID        string
	nodeAuthToken string
	node          nodev1.NodeControlClient
	operator      nodeoperatorv1.NodeOperatorClient
	runsc         runscConfig
	relay         relayConfig
	mu            sync.Mutex
	running       map[string]context.CancelFunc
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

func (d *daemon) run(ctx context.Context) error {
	var revision int64
	backoff := time.Second
	for {
		nextRevision, err := d.poll(ctx, revision)
		if err != nil {
			fmt.Fprintf(os.Stderr, "node-tunneld: watch poll failed: %v\n", err)
			if terminalControlError(err) {
				d.stopAll()
				return err
			}
		} else if nextRevision > revision {
			revision = nextRevision
			backoff = time.Second
		}
		wait := backoff + time.Duration(rand.Int63n(int64(backoff/2+time.Millisecond)))
		if err != nil && backoff < 30*time.Second {
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

func (d *daemon) poll(ctx context.Context, revision int64) (int64, error) {
	stream, err := d.node.WatchTunnelSessions(ctx, &nodev1.WatchTunnelSessionsRequest{NodeID: d.nodeID, NodeAuthToken: d.nodeAuthToken, AfterRevision: revision})
	if err != nil {
		return revision, err
	}
	resp, err := stream.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		return revision, err
	}
	if resp == nil {
		return revision, nil
	}
	for _, item := range resp.GetSessions() {
		if item.GetSession() != nil {
			if terminal(item.GetSession().GetStatus()) || item.GetSession().GetRevoked() {
				d.stopSession(item.GetSession().GetSessionID())
				continue
			}
			d.ensure(ctx, item)
		}
	}
	return resp.GetCurrentRevision(), nil
}

func terminalControlError(err error) bool {
	switch grpcstatus.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition:
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
		if err := d.serveSession(ctx, session, item.GetNodeToken()); err != nil {
			_, _ = d.node.ReportTunnelSessionStatus(context.Background(), &nodev1.ReportTunnelSessionStatusRequest{
				NodeID:        d.nodeID,
				NodeAuthToken: d.nodeAuthToken,
				SessionID:     session.GetSessionID(),
				Status:        statusForSessionError(err),
				Reason:        err.Error(),
			})
		}
	}()
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
