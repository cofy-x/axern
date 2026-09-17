package controlplane

import (
	"context"
	"time"

	gatewayv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/gateway/v1"
	"github.com/cofy-x/axern/lib/go/grpcclient"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Client struct {
	conn    *grpc.ClientConn
	Gateway gatewayv1.GatewayControlClient
}

func Dial(ctx context.Context, target, caPath, bundlePath, cluster string, timeout time.Duration, dialOptions ...grpc.DialOption) (*Client, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	options := []grpc.DialOption{
		grpc.WithTransportCredentials(&workloadtls.Credentials{BundlePath: bundlePath, TrustPath: caPath, Local: workloadtls.Identity{Cluster: cluster, Role: "gatewayd"}, Peer: workloadtls.Identity{Cluster: cluster, Role: "controld"}}), grpc.WithNoProxy(),
	}
	options = append(options, dialOptions...)
	conn, err := grpcclient.NewReadyClient(dialCtx, target, options...)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:    conn,
		Gateway: gatewayv1.NewGatewayControlClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

func (c *Client) ResolveAllocationTerminal(ctx context.Context, in *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	return c.Gateway.ResolveAllocationTerminal(ctx, in)
}

func (c *Client) AuthorizeAllocationAccess(ctx context.Context, in *gatewayv1.ResolveAllocationTerminalRequest) (*emptypb.Empty, error) {
	return c.Gateway.AuthorizeAllocationAccess(ctx, in)
}

func (c *Client) ResolveTunnelRelayTarget(ctx context.Context, sessionID string) (string, error) {
	resp, err := c.Gateway.ResolveTunnelRelayTarget(ctx, &gatewayv1.ResolveTunnelRelayTargetRequest{SessionID: sessionID})
	if err != nil {
		return "", err
	}
	return resp.GetNodeEdgeTarget(), nil
}
