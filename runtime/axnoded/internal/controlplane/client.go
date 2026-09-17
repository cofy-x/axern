package controlplane

import (
	"context"
	"fmt"
	"strings"
	"sync"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/lib/go/grpcclient"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"google.golang.org/grpc"
)

type NodeControlClientProvider interface {
	Client(context.Context) (nodev1.NodeControlClient, error)
	Close() error
}

type nodeControlClientProvider struct {
	target string
	opts   []grpc.DialOption

	mu     sync.Mutex
	conn   *grpc.ClientConn
	client nodev1.NodeControlClient
}

func NewNodeControlClientProvider(target, trustPath, bundlePath, cluster, nodeID string) (NodeControlClientProvider, error) {
	local := workloadtls.Identity{Cluster: cluster, Role: "axnoded", NodeID: nodeID}
	if _, err := local.URI(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(target) == "" || strings.TrimSpace(trustPath) == "" || strings.TrimSpace(bundlePath) == "" {
		return nil, fmt.Errorf("Node control target, trust and identity bundle are required")
	}
	opts := []grpc.DialOption{grpc.WithTransportCredentials(&workloadtls.Credentials{BundlePath: bundlePath, TrustPath: trustPath, Local: local, Peer: workloadtls.Identity{Cluster: cluster, Role: "controld"}}), grpc.WithNoProxy()}
	opts = append(opts, sdkobs.GRPCDialOptions()...)
	return &nodeControlClientProvider{target: target, opts: opts}, nil
}

func (p *nodeControlClientProvider) Client(ctx context.Context) (nodev1.NodeControlClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		return p.client, nil
	}
	conn, err := grpcclient.NewReadyClient(ctx, p.target, p.opts...)
	if err != nil {
		return nil, err
	}
	p.conn = conn
	p.client = nodev1.NewNodeControlClient(conn)
	return p.client, nil
}

func (p *nodeControlClientProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return nil
	}
	err := p.conn.Close()
	p.conn = nil
	p.client = nil
	return err
}
