package nodebridge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Dialer struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
	obs   *sdkobs.Handle
	creds credentials.TransportCredentials
}

func NewDialer(caCert, certPath, keyPath, serverName string, obs *sdkobs.Handle) (*Dialer, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load gateway node mTLS key pair: %w", err)
	}
	caPEM, err := os.ReadFile(caCert)
	if err != nil {
		return nil, fmt.Errorf("read gateway node mTLS CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse gateway node mTLS CA %q", caCert)
	}
	return &Dialer{
		conns: make(map[string]*grpc.ClientConn),
		obs:   obs,
		creds: credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: serverName}),
	}, nil
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
		options := []grpc.DialOption{grpc.WithTransportCredentials(d.creds.Clone())}
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

func (d *Dialer) Process(ctx context.Context, target string) (nodesandboxv1.NodeSandbox_ProcessClient, error) {
	client, err := d.client(ctx, target)
	if err != nil {
		return nil, err
	}
	return client.Process(ctx)
}
