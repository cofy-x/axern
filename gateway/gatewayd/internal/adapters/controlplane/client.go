package controlplane

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Client struct {
	conn    *grpc.ClientConn
	Gateway gatewayv1.GatewayControlClient
}

func Dial(ctx context.Context, target, caPath, certPath, keyPath string, timeout time.Duration, dialOptions ...grpc.DialOption) (*Client, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load tls key pair: %w", err)
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read tls ca cert: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse tls ca cert %q", caPath)
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	options := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion:   tls.VersionTLS12,
			RootCAs:      roots,
			Certificates: []tls.Certificate{cert},
		})),
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

func (c *Client) ResolveServiceRoute(ctx context.Context, in *gatewayv1.ResolveServiceRouteRequest) (*gatewayv1.ResolveServiceRouteResponse, error) {
	return c.Gateway.ResolveServiceRoute(ctx, in)
}

func (c *Client) ResolveAllocationTerminal(ctx context.Context, in *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	return c.Gateway.ResolveAllocationTerminal(ctx, in)
}

func (c *Client) ResolveTunnelRelayTarget(ctx context.Context, sessionID string) (string, error) {
	resp, err := c.Gateway.ResolveTunnelRelayTarget(ctx, &gatewayv1.ResolveTunnelRelayTargetRequest{SessionID: sessionID})
	if err != nil {
		return "", err
	}
	return resp.GetNodeEdgeTarget(), nil
}
