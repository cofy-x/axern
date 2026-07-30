package pgtunnel

import (
	"context"
	"errors"
	"fmt"
	"strings"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) Create(ctx context.Context, params tunnelkernel.CreateParams) (*tunnelkernel.CreateResult, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	allocationID := strings.TrimSpace(params.AllocationID)
	if allocationID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	if params.RemotePort != nil && (*params.RemotePort <= 0 || *params.RemotePort > 65535) {
		return nil, grpcstatus.Error(codes.InvalidArgument, "remote_port must be in 1..65535 when set")
	}
	localTarget := strings.TrimSpace(params.LocalTarget)
	if localTarget == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "local_target is required")
	}
	now := params.Now.UTC()
	ttl := normalizeTTL(params.TTL)
	expiresAt := now.Add(ttl)

	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tunnel session transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := expireDueTx(ctx, tx, now); err != nil {
		return nil, err
	}

	alloc, err := lookupAllocation(ctx, tx, allocationID)
	if err != nil {
		return nil, err
	}
	if alloc.Status != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String() {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "allocation is not running")
	}
	actor, ok := accesskernel.ActorFromContext(ctx)
	if !ok || strings.TrimSpace(actor.Principal.ID) == "" {
		return nil, grpcstatus.Error(codes.Unauthenticated, "authenticated principal is required")
	}
	var remotePort int32
	if params.RemotePort == nil {
		remotePort, err = allocateRemotePort(ctx, tx, allocationID)
		if err != nil {
			return nil, err
		}
	} else {
		remotePort = *params.RemotePort
	}
	sessionID, err := randomID("tun")
	if err != nil {
		return nil, err
	}
	clientToken, err := randomToken()
	if err != nil {
		return nil, err
	}
	nodeToken, err := randomToken()
	if err != nil {
		return nil, err
	}
	nodeTokenEncrypted, err := s.encryptNodeToken(nodeToken)
	if err != nil {
		return nil, err
	}
	relay, err := s.selectRelay(sessionID)
	if err != nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, err.Error())
	}
	revision, err := nextRevision(ctx, tx)
	if err != nil {
		return nil, err
	}
	session := &tunnelv1.TunnelSession{
		SessionID:          sessionID,
		AllocationID:       allocationID,
		Namespace:          alloc.Namespace,
		CreatorPrincipalID: actor.Principal.ID,
		NodeID:             alloc.NodeID,
		NodeTarget:         alloc.NodeTarget,
		Attempt:            alloc.Attempt,
		RemotePort:         remotePort,
		LocalTarget:        localTarget,
		EdgeTarget:         relay.ClientTarget,
		NodeEdgeTarget:     relay.NodeTarget,
		RelayID:            relay.ID,
		ClientEdgeTarget:   relay.ClientTarget,
		Status:             tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING,
		CreatedAt:          timestamppb.New(now),
		UpdatedAt:          timestamppb.New(now),
		ExpiresAt:          timestamppb.New(expiresAt),
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tunnel_sessions (
			session_id, allocation_id, namespace, creator_principal_id, node_id, node_target, attempt, remote_port,
			local_target, edge_target, node_edge_target, relay_id, client_edge_target, status, reason, bound_addr, revoked,
			client_token_hash, node_token_encrypted, node_token_hash, revision, created_at, updated_at, expires_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'','',FALSE,$15,$16,$17,$18,$19,$20,$21)
	`, session.GetSessionID(), session.GetAllocationID(), session.GetNamespace(), session.GetCreatorPrincipalID(), session.GetNodeID(), session.GetNodeTarget(), session.GetAttempt(), session.GetRemotePort(), session.GetLocalTarget(), session.GetEdgeTarget(), session.GetNodeEdgeTarget(), session.GetRelayID(), session.GetClientEdgeTarget(), session.GetStatus().String(), hashToken(clientToken), nodeTokenEncrypted, hashToken(nodeToken), revision, now, now, expiresAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, grpcstatus.Error(codes.AlreadyExists, "active tunnel session already binds this allocation remote_port")
		}
		return nil, fmt.Errorf("insert tunnel session: %w", err)
	}
	if err := s.insertEventTx(ctx, tx, eventParams{
		SessionID:  session.GetSessionID(),
		Type:       tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CREATED,
		Status:     session.GetStatus(),
		ReasonCode: tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_CREATED,
		Now:        now,
	}); err != nil {
		return nil, fmt.Errorf("insert tunnel created event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tunnel session: %w", err)
	}
	return &tunnelkernel.CreateResult{Session: session, ClientToken: clientToken, NodeToken: nodeToken}, nil
}
