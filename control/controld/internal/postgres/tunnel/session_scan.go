package pgtunnel

import (
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func sessionSelectColumns() string {
	return `session_id, allocation_id, node_id, node_target, attempt, remote_port, local_target, edge_target, node_edge_target, relay_id, client_edge_target, status, reason, bound_addr, revoked, client_token_hash, node_token_encrypted, node_token_hash, created_at, updated_at, expires_at, ready_at, last_peer_event_at, bytes_in, bytes_out`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(row rowScanner) (*tunnelv1.TunnelSession, string, []byte, string, error) {
	var (
		session                  tunnelv1.TunnelSession
		statusText               string
		clientHash               string
		nodeTokenEncrypted       []byte
		nodeHash                 string
		createdAt, updatedAt     time.Time
		expiresAt                time.Time
		readyAt, lastPeerEventAt *time.Time
	)
	err := row.Scan(&session.SessionID, &session.AllocationID, &session.NodeID, &session.NodeTarget, &session.Attempt, &session.RemotePort, &session.LocalTarget, &session.EdgeTarget, &session.NodeEdgeTarget, &session.RelayID, &session.ClientEdgeTarget, &statusText, &session.Reason, &session.BoundAddr, &session.Revoked, &clientHash, &nodeTokenEncrypted, &nodeHash, &createdAt, &updatedAt, &expiresAt, &readyAt, &lastPeerEventAt, &session.BytesIn, &session.BytesOut)
	if err != nil {
		return nil, "", nil, "", err
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
	return &session, clientHash, nodeTokenEncrypted, nodeHash, nil
}

func parseStatus(value string) tunnelv1.TunnelSessionStatus {
	if n, ok := tunnelv1.TunnelSessionStatus_value[value]; ok {
		return tunnelv1.TunnelSessionStatus(n)
	}
	return tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_UNSPECIFIED
}
