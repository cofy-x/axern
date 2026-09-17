package nodebridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	privatenodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"github.com/cofy-x/axern/lib/go/grpcclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

type LifecycleClient interface {
	CreateAllocation(context.Context, string, *privatenodev1.CreateAllocationRequest) (*privatenodev1.CreateAllocationResponse, error)
	DeleteAllocation(context.Context, string, *privatenodev1.DeleteAllocationRequest) (*privatenodev1.DeleteAllocationResponse, error)
	GetAllocationLifecycle(context.Context, string, *privatenodev1.GetAllocationLifecycleRequest) (*privatenodev1.GetAllocationLifecycleResponse, error)
	Close() error
}

const idempotentRPCAttempts = 2

type nodeEndpoint struct{ target, nodeID string }

type GRPCClient struct {
	mu    sync.Mutex
	conns map[nodeEndpoint]*grpc.ClientConn
	creds func(string) credentials.TransportCredentials
}

func NewGRPCClient(creds func(string) credentials.TransportCredentials) *GRPCClient {
	return &GRPCClient{conns: make(map[nodeEndpoint]*grpc.ClientConn), creds: creds}
}

func (c *GRPCClient) CreateAllocation(ctx context.Context, target string, req *privatenodev1.CreateAllocationRequest) (*privatenodev1.CreateAllocationResponse, error) {
	client, conn, err := c.clientConn(ctx, target, req.GetNodeID())
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateAllocation(ctx, req)
	if err != nil {
		c.discardRecoverableConn(nodeEndpoint{target, req.GetNodeID()}, conn, err)
		return nil, err
	}
	if resp.GetAllocationID() != req.GetAllocationID() {
		return nil, fmt.Errorf("node returned mismatched allocation %q", resp.GetAllocationID())
	}
	return resp, nil
}

func (c *GRPCClient) DeleteAllocation(ctx context.Context, target string, req *privatenodev1.DeleteAllocationRequest) (*privatenodev1.DeleteAllocationResponse, error) {
	var resp *privatenodev1.DeleteAllocationResponse
	var err error
	for range idempotentRPCAttempts {
		var client privatenodev1.NodeLifecycleClient
		var conn *grpc.ClientConn
		client, conn, err = c.clientConn(ctx, target, req.GetNodeID())
		if err != nil {
			return nil, err
		}
		resp, err = client.DeleteAllocation(ctx, req)
		c.discardRecoverableConn(nodeEndpoint{target, req.GetNodeID()}, conn, err)
		if !isRecoverableNodeRPCError(err) {
			return resp, err
		}
	}
	return resp, err
}

func (c *GRPCClient) GetAllocationLifecycle(ctx context.Context, target string, req *privatenodev1.GetAllocationLifecycleRequest) (*privatenodev1.GetAllocationLifecycleResponse, error) {
	var resp *privatenodev1.GetAllocationLifecycleResponse
	var err error
	for range idempotentRPCAttempts {
		var client privatenodev1.NodeLifecycleClient
		var conn *grpc.ClientConn
		client, conn, err = c.clientConn(ctx, target, req.GetNodeID())
		if err != nil {
			return nil, err
		}
		resp, err = client.GetAllocationLifecycle(ctx, req)
		c.discardRecoverableConn(nodeEndpoint{target, req.GetNodeID()}, conn, err)
		if !isRecoverableNodeRPCError(err) {
			return resp, err
		}
	}
	return resp, err
}

func (c *GRPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var first error
	for target, conn := range c.conns {
		if err := conn.Close(); err != nil && first == nil {
			first = err
		}
		delete(c.conns, target)
	}
	return first
}

func (c *GRPCClient) client(ctx context.Context, target, nodeID string) (privatenodev1.NodeLifecycleClient, error) {
	client, _, err := c.clientConn(ctx, target, nodeID)
	return client, err
}

func (c *GRPCClient) clientConn(ctx context.Context, target, nodeID string) (privatenodev1.NodeLifecycleClient, *grpc.ClientConn, error) {
	if c.creds == nil {
		return nil, nil, fmt.Errorf("Node transport factory is required")
	}
	endpoint := nodeEndpoint{target, nodeID}
	c.mu.Lock()
	conn := c.conns[endpoint]
	c.mu.Unlock()
	if conn == nil {
		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var err error
		conn, err = dial(dialCtx, target, c.creds(nodeID))
		if err != nil {
			return nil, nil, err
		}
		c.mu.Lock()
		if existing := c.conns[endpoint]; existing != nil {
			_ = conn.Close()
			conn = existing
		} else {
			c.conns[endpoint] = conn
		}
		c.mu.Unlock()
	}
	return privatenodev1.NewNodeLifecycleClient(conn), conn, nil
}

func (c *GRPCClient) discardRecoverableConn(target nodeEndpoint, conn *grpc.ClientConn, err error) {
	if conn == nil || !isRecoverableNodeRPCError(err) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conns[target] != conn {
		return
	}
	delete(c.conns, target)
	_ = conn.Close()
}

func isRecoverableNodeRPCError(err error) bool {
	return status.Code(err) == codes.Unavailable
}

func dial(ctx context.Context, target string, creds credentials.TransportCredentials) (*grpc.ClientConn, error) {
	if creds == nil {
		return nil, fmt.Errorf("node mTLS transport credentials are required")
	}
	conn, err := grpcclient.NewReadyClient(
		ctx,
		target,
		grpc.WithNoProxy(),
		grpc.WithTransportCredentials(creds.Clone()),
	)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
