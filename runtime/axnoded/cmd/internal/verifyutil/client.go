package verifyutil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
)

const dialTimeout = 15 * time.Second

type NodeClients struct {
	Lifecycle privatenodev1.NodeLifecycleClient
	Node      nodesandboxv1.NodeSandboxClient
	Health    healthgrpc.HealthClient
	conn      *grpc.ClientConn
}

type SandboxHandle struct {
	clients    *NodeClients
	SandboxID  string
	Attempt    int64
	LeaseToken string
}

func DialGRPC(address string) (*grpc.ClientConn, error) {
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		if filepath.IsAbs(addr) || len(addr) > 104 {
			if len(addr) > 104 {
				targetPath := filepath.Join(os.TempDir(), filepath.Base(addr))
				if _, err := os.Lstat(targetPath); os.IsNotExist(err) {
					if err := os.Symlink(addr, targetPath); err != nil && !errors.Is(err, os.ErrExist) {
						return nil, fmt.Errorf("create symlink: %w", err)
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
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	return grpcclient.NewReadyClient(
		dialCtx,
		grpcclient.PassthroughTarget(address),
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func DialNodeClients(address string) (*NodeClients, error) {
	conn, err := DialGRPC(address)
	if err != nil {
		return nil, err
	}
	return &NodeClients{
		Lifecycle: privatenodev1.NewNodeLifecycleClient(conn),
		Node:      nodesandboxv1.NewNodeSandboxClient(conn),
		Health:    healthgrpc.NewHealthClient(conn),
		conn:      conn,
	}, nil
}

func (c *NodeClients) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func NewSandboxID(prefix string) string {
	base := prefix
	if base == "" {
		base = "verify"
	}
	return fmt.Sprintf("axctl-%s-%s", base, uuid.NewString()[:8])
}

func CreateAllocation(ctx context.Context, clients *NodeClients, sandboxID string, spec *privatenodev1.ResolvedExecutionConfig) (*SandboxHandle, error) {
	return CreateAllocationWithAttempt(ctx, clients, sandboxID, 1, spec)
}

func CreateAllocationWithAttempt(ctx context.Context, clients *NodeClients, sandboxID string, attempt int64, spec *privatenodev1.ResolvedExecutionConfig) (*SandboxHandle, error) {
	if sandboxID == "" {
		sandboxID = NewSandboxID("verify")
	}
	req := &privatenodev1.CreateAllocationRequest{
		AllocationID: sandboxID,
		Attempt:      attempt,
		NodeID:       "",
		Config:       spec,
	}
	resp, err := clients.Lifecycle.CreateAllocation(ctx, req)
	if err != nil {
		return nil, err
	}
	return &SandboxHandle{
		clients:    clients,
		SandboxID:  resp.GetAllocationID(),
		Attempt:    resp.GetAttempt(),
		LeaseToken: "verify-local-lease",
	}, nil
}

func GetAllocationStatus(ctx context.Context, clients *NodeClients, sandboxID string) (*privatenodev1.GetAllocationStatusResponse, error) {
	return clients.Lifecycle.GetAllocationStatus(ctx, &privatenodev1.GetAllocationStatusRequest{
		AllocationID: sandboxID,
		Attempt:      1,
	})
}

func (h *SandboxHandle) Exec(ctx context.Context, spec *nodesandboxv1.ExecSpec) (*nodesandboxv1.ExecResponse, error) {
	return h.clients.Node.Exec(ctx, &nodesandboxv1.ExecRequest{
		AllocationID:        h.SandboxID,
		Attempt:             h.Attempt,
		ExecutionLeaseToken: h.LeaseToken,
		Spec:                spec,
	})
}

func (h *SandboxHandle) Wait(ctx context.Context) (*nodesandboxv1.WaitSandboxResponse, error) {
	return h.clients.Node.WaitSandbox(ctx, &nodesandboxv1.WaitSandboxRequest{
		AllocationID:        h.SandboxID,
		Attempt:             h.Attempt,
		ExecutionLeaseToken: h.LeaseToken,
	})
}

func (h *SandboxHandle) Delete(ctx context.Context, timeoutSeconds int64) error {
	_, err := h.clients.Lifecycle.DeleteAllocation(ctx, &privatenodev1.DeleteAllocationRequest{
		AllocationID:   h.SandboxID,
		Attempt:        h.Attempt,
		TimeoutSeconds: timeoutSeconds,
	})
	return err
}
