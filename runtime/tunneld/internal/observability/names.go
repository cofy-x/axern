package observability

import sdkobs "github.com/cofy-x/axern/lib/go/observability"

const (
	AttrPeerKind    = "axern.tunnel_peer_kind"
	AttrFrameKind   = "axern.tunnel_frame_kind"
	AttrRelayID     = "axern.tunnel_relay_id"
	AttrCloseReason = "axern.tunnel_close_reason"
)

var (
	MetricRelayPeerConnectTotal = sdkobs.Instrument{
		Name:        "axern.tunneld_peer_connect_total",
		Description: "Tunnel relay peer connection attempts by kind and result.",
	}
	MetricRelayPeerDisconnectTotal = sdkobs.Instrument{
		Name:        "axern.tunneld_peer_disconnect_total",
		Description: "Tunnel relay peer disconnections by kind and result.",
	}
	MetricRelayFrameForwardTotal = sdkobs.Instrument{
		Name:        "axern.tunneld_frame_forward_total",
		Description: "Tunnel relay frames forwarded between peers by frame kind.",
	}
	MetricRelayBytesForwardTotal = sdkobs.Instrument{
		Name:        "axern.tunneld_bytes_forward_total",
		Description: "Tunnel relay stream payload bytes forwarded between peers.",
	}
	MetricRelayActiveSessions = sdkobs.Instrument{
		Name:        "axern.tunneld_active_sessions",
		Description: "Current active tunnel relay session pair slots.",
	}
	MetricRelayActivePeers = sdkobs.Instrument{
		Name:        "axern.tunneld_active_peers",
		Description: "Current active tunnel relay peers by kind.",
	}
)
