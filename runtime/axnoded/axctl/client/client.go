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
	resp, err := c.healthzClient.Check(ctx, &healthgrpc.HealthCheckRequest{
		Service: nodeoperatorv1.NodeOperator_ServiceDesc.ServiceName,
	})
	if err != nil {
		return err.Error()
	}
	return healthgrpc.HealthCheckResponse_ServingStatus_name[int32(resp.Status)]
}

func (c *Client) ListAllocations() (*nodeoperatorv1.ListAllocationsResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.ListAllocations(ctx, &nodeoperatorv1.ListAllocationsRequest{})
}

func (c *Client) GetAllocation(allocationID string) (*nodeoperatorv1.GetAllocationResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.GetAllocation(ctx, &nodeoperatorv1.GetAllocationRequest{AllocationID: allocationID})
}

func (c *Client) GetAllocationDiagnostics(allocationID string, full bool) (*nodeoperatorv1.GetAllocationDiagnosticsResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.GetAllocationDiagnostics(ctx, &nodeoperatorv1.GetAllocationDiagnosticsRequest{AllocationID: allocationID, Full: full})
}

func (c *Client) GetAllocationMemory(allocationID string) (*nodeoperatorv1.GetAllocationMemoryResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.GetAllocationMemory(ctx, &nodeoperatorv1.GetAllocationMemoryRequest{AllocationID: allocationID})
}

func (c *Client) ExplainAllocationNetworkPolicy(allocationID string) (*nodeoperatorv1.ExplainAllocationNetworkPolicyResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.ExplainAllocationNetworkPolicy(ctx, &nodeoperatorv1.ExplainAllocationNetworkPolicyRequest{AllocationID: allocationID})
}

func (c *Client) ForceCleanupAllocation(allocationID, reason string, timeoutSeconds int64) (*nodeoperatorv1.ForceCleanupAllocationResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.deleteRPCTimeout(timeoutSeconds))
	defer cancel()
	return c.operatorClient.ForceCleanupAllocation(ctx, &nodeoperatorv1.ForceCleanupAllocationRequest{
		AllocationID:   allocationID,
		Reason:         reason,
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

func (c *Client) ForceTerminateAllocation(allocationID, reason string) (*nodeoperatorv1.ForceTerminateAllocationResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.operatorClient.ForceTerminateAllocation(ctx, &nodeoperatorv1.ForceTerminateAllocationRequest{
		AllocationID: allocationID,
		Reason:       reason,
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

func (c *Client) Wait(allocationID string, timeout time.Duration) (*nodeoperatorv1.WaitResponse, error) {
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
	return c.operatorClient.Wait(ctx, &nodeoperatorv1.WaitRequest{AllocationID: allocationID})
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
