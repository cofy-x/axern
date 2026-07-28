package nodebridge

import (
	"context"
	"sync"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Dialer struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
	obs   *sdkobs.Handle
}

func NewDialer(obs *sdkobs.Handle) *Dialer {
	return &Dialer{conns: make(map[string]*grpc.ClientConn), obs: obs}
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

func (d *Dialer) client(ctx context.Context, target string) (nodesandboxv1.NodeSandboxClient, error) {
	d.mu.Lock()
	conn := d.conns[target]
	d.mu.Unlock()
	if conn == nil {
		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var err error
		options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
		if d.obs != nil {
			options = append(options, d.obs.GRPCDialOptions()...)
		}
		conn, err = grpcclient.NewReadyClient(dialCtx, target, options...)
		if err != nil {
			return nil, err
		}
		d.mu.Lock()
		if existing := d.conns[target]; existing != nil {
			_ = conn.Close()
			conn = existing
		} else {
			d.conns[target] = conn
		}
		d.mu.Unlock()
	}
	return nodesandboxv1.NewNodeSandboxClient(conn), nil
}

func (d *Dialer) NodeSandbox(ctx context.Context, target string) (nodesandboxv1.NodeSandboxClient, error) {
	return d.client(ctx, target)
}

func (d *Dialer) ExecStream(ctx context.Context, target string) (nodesandboxv1.NodeSandbox_ExecStreamClient, error) {
	client, err := d.client(ctx, target)
	if err != nil {
		return nil, err
	}
	return client.ExecStream(ctx)
}
