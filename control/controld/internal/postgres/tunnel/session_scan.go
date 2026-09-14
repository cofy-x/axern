package pgtunnel

import (
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func sessionSelectColumns() string {
	return `session_id, allocation_id, namespace, creator_principal_id, node_id, remote_port, relay_id, client_edge_target, status, reason, bound_addr, client_token_hash, node_token_encrypted, node_token_hash, node_edge_target, created_at, updated_at, expires_at, ready_at, last_peer_event_at, bytes_in, bytes_out`
}

type sessionInternal struct {
	clientTokenHash string
	nodeTokenCipher []byte
	nodeTokenHash   string
	nodeEdgeTarget  string
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(row rowScanner) (*tunnelv1.TunnelSession, sessionInternal, error) {
	var (
		session                  tunnelv1.TunnelSession
		statusText               string
		internal                 sessionInternal
		createdAt, updatedAt     time.Time
		expiresAt                time.Time
		readyAt, lastPeerEventAt *time.Time
	)
	err := row.Scan(&session.SessionID, &session.AllocationID, &session.Namespace, &session.CreatorPrincipalID, &session.NodeID, &session.RemotePort, &session.RelayID, &session.ClientEdgeTarget, &statusText, &session.Reason, &session.BoundAddr, &internal.clientTokenHash, &internal.nodeTokenCipher, &internal.nodeTokenHash, &internal.nodeEdgeTarget, &createdAt, &updatedAt, &expiresAt, &readyAt, &lastPeerEventAt, &session.BytesIn, &session.BytesOut)
	if err != nil {
		return nil, sessionInternal{}, err
	}
	session.Status = parseStatus(statusText)
	session.CreatedAt = timestamppb.New(createdAt)
	session.UpdatedAt = timestamppb.New(updatedAt)
	session.ExpiresAt = timestamppb.New(expiresAt)
	if readyAt != nil {
		session.ReadyAt = timestamppb.New(*readyAt)
	}
	if lastPeerEventAt != nil {
		session.LastPeerEventAt = timestamppb.New(*lastPeerEventAt)
	}
	return &session, internal, nil
}

func parseStatus(value string) tunnelv1.TunnelSessionStatus {
	if n, ok := tunnelv1.TunnelSessionStatus_value[value]; ok {
		return tunnelv1.TunnelSessionStatus(n)
	}
	return tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_UNSPECIFIED
}
