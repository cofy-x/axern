package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/urfave/cli"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
)

type Client struct {
	operatorClient nodeoperatorv1.NodeOperatorClient
	healthzClient  healthgrpc.HealthClient
	conn           *grpc.ClientConn
	timeout        time.Duration
}

const deleteRPCGraceBuffer = 10 * time.Second

func New(ctx *cli.Context) (*Client, error) {
	duration := ctx.GlobalDuration("timeout")
	address, err := normalizeLocalSocketPath(ctx.GlobalString("address"))
	if err != nil {
		return nil, err
	}
	conn, err := dialLocalSocket(address)
	if err != nil {
		return nil, err
	}
	return &Client{
		operatorClient: nodeoperatorv1.NewNodeOperatorClient(conn),
		healthzClient:  healthgrpc.NewHealthClient(conn),
		conn:           conn,
		timeout:        duration,
	}, nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) Healthz() string {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	resp, err := c.healthzClient.Check(ctx, &healthgrpc.HealthCheckRequest{Service: "sandbox"})
	if err != nil {
		return err.Error()
	}
	return healthgrpc.HealthCheckResponse_ServingStatus_name[int32(resp.Status)]
}

func (c *Client) ListSandboxes() (*nodeoperatorv1.ListSandboxesResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.ListSandboxes(ctx, &nodeoperatorv1.ListSandboxesRequest{})
}

func (c *Client) GetSandbox(sandboxID string) (*nodeoperatorv1.GetSandboxResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.GetSandbox(ctx, &nodeoperatorv1.GetSandboxRequest{SandboxID: sandboxID})
}

func (c *Client) GetSandboxDiagnostics(sandboxID string, full bool) (*nodeoperatorv1.GetSandboxDiagnosticsResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.GetSandboxDiagnostics(ctx, &nodeoperatorv1.GetSandboxDiagnosticsRequest{SandboxID: sandboxID, Full: full})
}

func (c *Client) GetSandboxMemory(sandboxID string) (*nodeoperatorv1.GetSandboxMemoryResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.GetSandboxMemory(ctx, &nodeoperatorv1.GetSandboxMemoryRequest{SandboxID: sandboxID})
}

func (c *Client) DeleteSandbox(sandboxID string, timeoutSeconds int64) (*nodeoperatorv1.DeleteSandboxResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.deleteRPCTimeout(timeoutSeconds))
	defer cancel()
	return c.operatorClient.DeleteSandbox(ctx, &nodeoperatorv1.DeleteSandboxRequest{
		SandboxID:      sandboxID,
		TimeoutSeconds: timeoutSeconds,
	})
}

func (c *Client) deleteRPCTimeout(timeoutSeconds int64) time.Duration {
	if timeoutSeconds <= 0 {
		return c.timeout
	}

	minimum := time.Duration(timeoutSeconds)*time.Second + deleteRPCGraceBuffer
	switch {
	case c.timeout <= 0:
		return minimum
	case c.timeout < minimum:
		return minimum
	default:
		return c.timeout
	}
}

func (c *Client) KillSandbox(sandboxID, signal string) (*nodeoperatorv1.KillSandboxResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.KillSandbox(ctx, &nodeoperatorv1.KillSandboxRequest{
		SandboxID: sandboxID,
		Signal:    signal,
	})
}

func (c *Client) Exec(req *nodeoperatorv1.ExecRequest) (*nodeoperatorv1.ExecResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.Exec(ctx, req)
}

func (c *Client) ExecStream(timeout time.Duration) (nodeoperatorv1.NodeOperator_ExecStreamClient, context.CancelFunc, error) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	stream, err := c.operatorClient.ExecStream(ctx)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return stream, cancel, nil
}

func (c *Client) WaitSandbox(sandboxID string, timeout time.Duration) (*nodeoperatorv1.WaitSandboxResponse, error) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()
	return c.operatorClient.WaitSandbox(ctx, &nodeoperatorv1.WaitSandboxRequest{SandboxID: sandboxID})
}

func normalizeLocalSocketPath(address string) (string, error) {
	cleaned := strings.TrimSpace(address)
	if cleaned == "" {
		return "", fmt.Errorf("local unix socket path is required")
	}
	if after, ok := strings.CutPrefix(cleaned, "unix://"); ok {
		cleaned = after
	}
	if strings.Contains(cleaned, "://") || !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("axctl only supports local unix socket paths; got %q", address)
	}
	return cleaned, nil
}

func dialLocalSocket(address string) (*grpc.ClientConn, error) {
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		if len(addr) > 104 {
			targetPath := filepath.Join(os.TempDir(), filepath.Base(addr))
			if _, err := os.Lstat(targetPath); os.IsNotExist(err) {
				if err := os.Symlink(addr, targetPath); err != nil && !errors.Is(err, os.ErrExist) {
					return nil, fmt.Errorf("create socket symlink: %w", err)
				}
			}
			addr = targetPath
		}
		unixAddr, err := net.ResolveUnixAddr("unix", addr)
		if err != nil {
			return nil, err
		}
		return net.DialUnix("unix", nil, unixAddr)
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return grpcclient.NewReadyClient(
		dialCtx,
		grpcclient.PassthroughTarget(address),
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}
