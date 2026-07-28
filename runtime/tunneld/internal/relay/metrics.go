package relay

import (
	"context"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	tunneldobs "github.com/cofy-x/axern/runtime/tunneld/internal/observability"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"go.opentelemetry.io/otel/attribute"
)

func (s *Server) ObserveActiveSessions(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	if observe == nil {
		return nil
	}
	observe(s.registry.sessions.Load(), attribute.String(tunneldobs.AttrRelayID, s.relayID))
	return nil
}

func (s *Server) ObserveActivePeers(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	if observe == nil {
		return nil
	}
	clientPeers := s.registry.clientPeer.Load()
	nodePeers := s.registry.nodePeer.Load()
	observe(clientPeers, attribute.String(tunneldobs.AttrRelayID, s.relayID), attribute.String(tunneldobs.AttrPeerKind, peerKindLabel(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)))
	observe(nodePeers, attribute.String(tunneldobs.AttrRelayID, s.relayID), attribute.String(tunneldobs.AttrPeerKind, peerKindLabel(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE)))
	return nil
}

func (s *Server) recordPeerConnect(ctx context.Context, kind tunnelcontrolv1.TunnelPeerKind, result string) {
	sdkobs.Int64Counter(tunneldobs.MetricRelayPeerConnectTotal.Name, tunneldobs.MetricRelayPeerConnectTotal.Description).Add(ctx, 1,
		attribute.String(tunneldobs.AttrRelayID, s.relayID),
		attribute.String(tunneldobs.AttrPeerKind, peerKindLabel(kind)),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func (s *Server) recordPeerDisconnect(ctx context.Context, kind tunnelcontrolv1.TunnelPeerKind, result, closeReason string) {
	sdkobs.Int64Counter(tunneldobs.MetricRelayPeerDisconnectTotal.Name, tunneldobs.MetricRelayPeerDisconnectTotal.Description).Add(ctx, 1,
		attribute.String(tunneldobs.AttrRelayID, s.relayID),
		attribute.String(tunneldobs.AttrPeerKind, peerKindLabel(kind)),
		attribute.String(tunneldobs.AttrCloseReason, closeReason),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func (s *Server) recordFrameForwarded(ctx context.Context, frame *tunnelv1.TunnelFrame) {
	kind := frameKind(frame)
	sdkobs.Int64Counter(tunneldobs.MetricRelayFrameForwardTotal.Name, tunneldobs.MetricRelayFrameForwardTotal.Description).Add(ctx, 1,
		attribute.String(tunneldobs.AttrRelayID, s.relayID),
		attribute.String(tunneldobs.AttrFrameKind, kind),
	)
	if data := frame.GetStreamData(); data != nil {
		sdkobs.Int64Counter(tunneldobs.MetricRelayBytesForwardTotal.Name, tunneldobs.MetricRelayBytesForwardTotal.Description).Add(ctx, int64(len(data.GetData())),
			attribute.String(tunneldobs.AttrRelayID, s.relayID),
			attribute.String(tunneldobs.AttrFrameKind, kind),
		)
	}
}
