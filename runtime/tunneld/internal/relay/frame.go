package relay

import (
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

func peerKindLabel(kind tunnelcontrolv1.TunnelPeerKind) string {
	switch kind {
	case tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT:
		return "client"
	case tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE:
		return "node"
	default:
		return "unspecified"
	}
}

func frameKind(frame *tunnelv1.TunnelFrame) string {
	if frame == nil {
		return "nil"
	}
	switch frame.GetPayload().(type) {
	case *tunnelv1.TunnelFrame_StreamOpen:
		return "stream_open"
	case *tunnelv1.TunnelFrame_StreamData:
		return "stream_data"
	case *tunnelv1.TunnelFrame_StreamClose:
		return "stream_close"
	case *tunnelv1.TunnelFrame_Ping:
		return "ping"
	case *tunnelv1.TunnelFrame_Pong:
		return "pong"
	case *tunnelv1.TunnelFrame_PeerOpen:
		return "peer_open"
	default:
		return "unknown"
	}
}
