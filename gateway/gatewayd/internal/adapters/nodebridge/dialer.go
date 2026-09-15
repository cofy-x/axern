package nodebridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc"
)

type nodeEndpoint struct{ target, nodeID string }

type Dialer struct {
	mu                             sync.Mutex
	conns                          map[nodeEndpoint]*grpc.ClientConn
	obs                            *sdkobs.Handle
	trustPath, bundlePath, cluster string
}

func NewDialer(trustPath, bundlePath, cluster string, obs *sdkobs.Handle) (*Dialer, error) {
	if _, err := (workloadtls.Identity{Cluster: cluster, Role: "gatewayd"}).URI(); err != nil {
		return nil, err
	}
	if trustPath == "" || bundlePath == "" {
		return nil, fmt.Errorf("workload trust and bundle are required")
	}
	return &Dialer{conns: make(map[nodeEndpoint]*grpc.ClientConn), obs: obs, trustPath: trustPath, bundlePath: bundlePath, cluster: cluster}, nil
}

func (d *Dialer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var first error
	for target, conn := range d.conns {
		if err := conn.Close(); err != nil && first == nil {
			first = err
		}
		delete(d.conns, target)
	}
	return first
}

func (d *Dialer) client(ctx context.Context, target, nodeID string) (nodesandboxv1.NodeSandboxClient, error) {
	identity := workloadtls.Identity{Cluster: d.cluster, Role: "axnoded", NodeID: nodeID}
	if _, err := identity.URI(); err != nil {
		return nil, err
	}
	endpoint := nodeEndpoint{target, nodeID}
	d.mu.Lock()
	conn := d.conns[endpoint]
	d.mu.Unlock()
	if conn == nil {
		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var err error
		options := []grpc.DialOption{grpc.WithTransportCredentials(&workloadtls.Credentials{BundlePath: d.bundlePath, TrustPath: d.trustPath, Local: workloadtls.Identity{Cluster: d.cluster, Role: "gatewayd"}, Peer: identity}), grpc.WithNoProxy()}
		if d.obs != nil {
			options = append(options, d.obs.GRPCDialOptions()...)
		}
		conn, err = grpcclient.NewReadyClient(dialCtx, target, options...)
		if err != nil {
			return nil, err
		}
		d.mu.Lock()
		if existing := d.conns[endpoint]; existing != nil {
			_ = conn.Close()
			conn = existing
		} else {
			d.conns[endpoint] = conn
		}
		d.mu.Unlock()
	}
	return nodesandboxv1.NewNodeSandboxClient(conn), nil
}

func (d *Dialer) NodeSandbox(ctx context.Context, target, nodeID string) (nodesandboxv1.NodeSandboxClient, error) {
	return d.client(ctx, target, nodeID)
}

func (d *Dialer) Process(ctx context.Context, target, nodeID string) (nodesandboxv1.NodeSandbox_ProcessClient, error) {
	client, err := d.client(ctx, target, nodeID)
	if err != nil {
		return nil, err
	}
	return client.Process(ctx)
}
