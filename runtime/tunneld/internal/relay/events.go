package relay

import (
	"context"
	"errors"
	"io"

	"github.com/cofy-x/axern/lib/go/observability"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	peerCloseReasonNormal         = "normal"
	peerCloseReasonContextCancel  = "context canceled"
	peerCloseReasonPongTimeout    = "pong timeout"
	peerCloseReasonFrameTooLarge  = "frame too large"
	peerCloseReasonQueueFull      = "queue full"
	peerCloseReasonOppositeMissed = "opposite missing"
)

type peerCloseOutcome struct {
	result     string
	reasonCode tunnelcontrolv1.TunnelSessionEventReasonCode
	reason     string
	err        error
}

func (s *Server) reportPeerEvent(ctx context.Context, sessionID, peerToken string, kind tunnelcontrolv1.TunnelPeerKind, eventType tunnelcontrolv1.TunnelSessionEventType, reasonCode tunnelcontrolv1.TunnelSessionEventReasonCode, reason string, bytesIn, bytesOut int64) {
	if s.control == nil || sessionID == "" {
		return
	}
	_, _ = s.control.ReportTunnelPeerEvent(ctx, &tunnelrelaycontrolv1.ReportTunnelPeerEventRequest{
		SessionID:  sessionID,
		RelayID:    s.relayID,
		PeerKind:   kind,
		EventType:  eventType,
		ReasonCode: reasonCode,
		Reason:     reason,
		BytesIn:    bytesIn,
		BytesOut:   bytesOut,
		PeerToken:  peerToken,
	})
}

func connectedEvent(kind tunnelcontrolv1.TunnelPeerKind) tunnelcontrolv1.TunnelSessionEventType {
	if kind == tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE {
		return tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_NODE_CONNECTED
	}
	return tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED
}

func disconnectedEvent(kind tunnelcontrolv1.TunnelPeerKind) tunnelcontrolv1.TunnelSessionEventType {
	if kind == tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE {
		return tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_NODE_DISCONNECTED
	}
	return tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_DISCONNECTED
}

func connectedReason(kind tunnelcontrolv1.TunnelPeerKind) tunnelcontrolv1.TunnelSessionEventReasonCode {
	if kind == tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE {
		return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_NODE_CONNECTED
	}
	return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_CLIENT_CONNECTED
}

func disconnectedReason(kind tunnelcontrolv1.TunnelPeerKind) tunnelcontrolv1.TunnelSessionEventReasonCode {
	if kind == tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE {
		return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_NODE_DISCONNECTED
	}
	return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_CLIENT_DISCONNECTED
}

func peerCloseOutcomeForError(kind tunnelcontrolv1.TunnelPeerKind, err error) peerCloseOutcome {
	if err == nil || errors.Is(err, io.EOF) {
		return peerCloseOutcome{
			result:     observability.ResultOK,
			reasonCode: disconnectedReason(kind),
			reason:     peerCloseReasonNormal,
		}
	}
	if grpcstatus.Code(err) == codes.Canceled {
		return peerCloseOutcome{
			result:     observability.ResultOK,
			reasonCode: disconnectedReason(kind),
			reason:     peerCloseReasonContextCancel,
		}
	}
	reasonCode, reason := classifyRelayError(err)
	return peerCloseOutcome{
		result:     observability.ResultError,
		reasonCode: reasonCode,
		reason:     reason,
		err:        err,
	}
}

func reasonForError(err error) tunnelcontrolv1.TunnelSessionEventReasonCode {
	reasonCode, _ := classifyRelayError(err)
	return reasonCode
}

func classifyRelayError(err error) (tunnelcontrolv1.TunnelSessionEventReasonCode, string) {
	switch grpcstatus.Code(err) {
	case codes.ResourceExhausted:
		if err != nil && err.Error() == "rpc error: code = ResourceExhausted desc = tunnel frame too large" {
			return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_FRAME_TOO_LARGE, peerCloseReasonFrameTooLarge
		}
		return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_QUEUE_FULL, peerCloseReasonQueueFull
	case codes.Unavailable:
		return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_OPPOSITE_MISSING, peerCloseReasonOppositeMissed
	case codes.DeadlineExceeded:
		return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_PONG_TIMEOUT, peerCloseReasonPongTimeout
	default:
		if err == nil {
			return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_UNSPECIFIED, peerCloseReasonNormal
		}
		return tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_UNSPECIFIED, err.Error()
	}
}
