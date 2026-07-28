package nodebridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type LifecycleClient interface {
	CreateAllocation(context.Context, string, *privatenodev1.CreateAllocationRequest) (*privatenodev1.CreateAllocationResponse, error)
	DeleteAllocation(context.Context, string, *privatenodev1.DeleteAllocationRequest) (*privatenodev1.DeleteAllocationResponse, error)
	GetAllocationStatus(context.Context, string, *privatenodev1.GetAllocationStatusRequest) (*privatenodev1.GetAllocationStatusResponse, error)
	DeleteVolume(context.Context, string, *privatenodev1.DeleteVolumeRequest) (*privatenodev1.DeleteVolumeResponse, error)
	Close() error
}

func (c *GRPCClient) DeleteVolume(ctx context.Context, target string, req *privatenodev1.DeleteVolumeRequest) (*privatenodev1.DeleteVolumeResponse, error) {
	var resp *privatenodev1.DeleteVolumeResponse
	var err error
	for range idempotentRPCAttempts {
		var client privatenodev1.NodeLifecycleClient
		var conn *grpc.ClientConn
		client, conn, err = c.clientConn(ctx, target)
		if err != nil {
			return nil, err
		}
		resp, err = client.DeleteVolume(ctx, req)
		c.discardRecoverableConn(target, conn, err)
		if !isRecoverableNodeRPCError(err) {
			return resp, err
		}
	}
	return resp, err
}

const idempotentRPCAttempts = 2

type GRPCClient struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

func NewGRPCClient() *GRPCClient {
	return &GRPCClient{conns: make(map[string]*grpc.ClientConn)}
}

func (c *GRPCClient) CreateAllocation(ctx context.Context, target string, req *privatenodev1.CreateAllocationRequest) (*privatenodev1.CreateAllocationResponse, error) {
	client, conn, err := c.clientConn(ctx, target)
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateAllocation(ctx, req)
	if err != nil {
		c.discardRecoverableConn(target, conn, err)
		return nil, err
	}
	if resp.GetAllocationID() != req.GetAllocationID() || resp.GetAttempt() != req.GetAttempt() {
		return nil, fmt.Errorf("node returned mismatched allocation %q/%d", resp.GetAllocationID(), resp.GetAttempt())
	}
	return resp, nil
}

func (c *GRPCClient) DeleteAllocation(ctx context.Context, target string, req *privatenodev1.DeleteAllocationRequest) (*privatenodev1.DeleteAllocationResponse, error) {
	var resp *privatenodev1.DeleteAllocationResponse
	var err error
	for range idempotentRPCAttempts {
		var client privatenodev1.NodeLifecycleClient
		var conn *grpc.ClientConn
		client, conn, err = c.clientConn(ctx, target)
		if err != nil {
			return nil, err
		}
		resp, err = client.DeleteAllocation(ctx, req)
		c.discardRecoverableConn(target, conn, err)
		if !isRecoverableNodeRPCError(err) {
			return resp, err
		}
	}
	return resp, err
}

func (c *GRPCClient) GetAllocationStatus(ctx context.Context, target string, req *privatenodev1.GetAllocationStatusRequest) (*privatenodev1.GetAllocationStatusResponse, error) {
	var resp *privatenodev1.GetAllocationStatusResponse
	var err error
	for range idempotentRPCAttempts {
		var client privatenodev1.NodeLifecycleClient
		var conn *grpc.ClientConn
		client, conn, err = c.clientConn(ctx, target)
		if err != nil {
			return nil, err
		}
		resp, err = client.GetAllocationStatus(ctx, req)
		c.discardRecoverableConn(target, conn, err)
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

func (c *GRPCClient) client(ctx context.Context, target string) (privatenodev1.NodeLifecycleClient, error) {
	client, _, err := c.clientConn(ctx, target)
	return client, err
}

func (c *GRPCClient) clientConn(ctx context.Context, target string) (privatenodev1.NodeLifecycleClient, *grpc.ClientConn, error) {
	c.mu.Lock()
	conn := c.conns[target]
	c.mu.Unlock()
	if conn == nil {
		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var err error
		conn, err = dial(dialCtx, target)
		if err != nil {
			return nil, nil, err
		}
		c.mu.Lock()
		if existing := c.conns[target]; existing != nil {
			_ = conn.Close()
			conn = existing
		} else {
			c.conns[target] = conn
		}
		c.mu.Unlock()
	}
	return privatenodev1.NewNodeLifecycleClient(conn), conn, nil
}

func (c *GRPCClient) discardRecoverableConn(target string, conn *grpc.ClientConn, err error) {
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

func dial(ctx context.Context, target string) (*grpc.ClientConn, error) {
	conn, err := grpcclient.NewReadyClient(
		ctx,
		target,
		grpc.WithNoProxy(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
