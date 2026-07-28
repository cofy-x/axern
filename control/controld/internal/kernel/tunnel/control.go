package tunnelkernel

import (
	"context"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

type Control interface {
	Create(ctx context.Context, params CreateParams) (*CreateResult, error)
	Get(ctx context.Context, sessionID string, now time.Time) (*tunnelv1.TunnelSession, error)
	List(ctx context.Context, allocationID, nodeID string, includeTerminal bool, now time.Time) ([]*tunnelv1.TunnelSession, error)
	ListEvents(ctx context.Context, sessionID string, limit int32, now time.Time) ([]*tunnelv1.TunnelSessionEvent, error)
	Revoke(ctx context.Context, sessionID, reason string, now time.Time) (*tunnelv1.TunnelSession, error)
	Renew(ctx context.Context, sessionID, clientToken string, ttl time.Duration, now time.Time) (*tunnelv1.TunnelSession, error)
	ValidatePeer(ctx context.Context, sessionID string, kind tunnelv1.TunnelPeerKind, token string, now time.Time) (*tunnelv1.TunnelSession, error)
}

type RelayControl interface {
	ReportPeerEvent(ctx context.Context, params PeerEventParams, now time.Time) (*tunnelv1.TunnelSession, error)
}

type PeerEventParams struct {
	SessionID  string
	RelayID    string
	PeerKind   tunnelv1.TunnelPeerKind
	EventType  tunnelv1.TunnelSessionEventType
	ReasonCode tunnelv1.TunnelSessionEventReasonCode
	Reason     string
	BytesIn    int64
	BytesOut   int64
	PeerToken  string
}

type NodeControl interface {
	WatchNode(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*nodev1.NodeTunnelSession, int64, error)
	ReportStatus(ctx context.Context, nodeID, sessionID string, status tunnelv1.TunnelSessionStatus, reason, boundAddr string, now time.Time) (*tunnelv1.TunnelSession, error)
}
