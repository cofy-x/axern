package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	tunnetns "github.com/cofy-x/axern/runtime/tunneld/internal/netns"
	"github.com/cofy-x/axern/runtime/tunneld/internal/relaytls"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

func (d *daemon) serveSession(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token string) error {
	network, err := d.operator.ResolveSandboxNetwork(ctx, &nodeoperatorv1.ResolveSandboxNetworkRequest{SandboxID: session.GetAllocationID()})
	if err != nil {
		return err
	}
	if network.GetRuntimeClass() == "runsc" {
		return d.serveRunscSession(ctx, session, token, network)
	}
	return d.serveNetnsSession(ctx, session, token, network)
}

func (d *daemon) serveNetnsSession(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token string, network *nodeoperatorv1.ResolveSandboxNetworkResponse) error {
	edgeTarget := session.GetNodeEdgeTarget()
	if edgeTarget == "" {
		return fmt.Errorf("session node_edge_target is required")
	}
	dialTarget, err := resolveSandboxReachableTarget(edgeTarget)
	if err != nil {
		return degradedSessionError(err)
	}
	relayServerName, err := serverNameFromTarget(edgeTarget)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "node-tunneld: connecting relay session=%s target=%s dial_target=%s\n", session.GetSessionID(), edgeTarget, dialTarget)
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dialOpts, err := relaytls.DialOptions(relaytls.ClientConfig{CACert: d.relay.caCert, ServerName: relayServerName})
	if err != nil {
		return err
	}
	conn, err := grpcclient.NewReadyClient(dialCtx, dialTarget, dialOpts...)
	if err != nil {
		return degradedSessionError(err)
	}
	defer conn.Close()
	stream, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(ctx)
	if err != nil {
		return degradedSessionError(err)
	}
	if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{
		SessionID: session.GetSessionID(),
		PeerKind:  tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE,
		Token:     token,
	}}}); err != nil {
		return degradedSessionError(err)
	}
	fmt.Fprintf(os.Stderr, "node-tunneld: relay connected session=%s\n", session.GetSessionID())

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(session.GetRemotePort())))
	ln, err := tunnetns.ListenTCPInPath(network.GetNetnsPath(), addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	_, _ = d.node.ReportTunnelSessionStatus(ctx, &nodev1.ReportTunnelSessionStatusRequest{
		NodeID:        d.nodeID,
		NodeAuthToken: d.nodeAuthToken,
		SessionID:     session.GetSessionID(),
		Status:        tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
		BoundAddr:     addr,
	})
	node := &nodePeer{stream: stream, listener: ln, conns: make(map[uint64]net.Conn)}
	node.nextID.Store(initialStreamCounter())
	return degradedSessionError(node.run(ctx))
}

func resolveSandboxReachableTarget(target string) (string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return "", err
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			return net.JoinHostPort(ip4.String(), port), nil
		}
	}
	if len(ips) > 0 {
		return net.JoinHostPort(ips[0].String(), port), nil
	}
	return "", fmt.Errorf("resolve %q: no addresses", host)
}

func serverNameFromTarget(target string) (string, error) {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return "", err
	}
	return host, nil
}
