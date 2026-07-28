package controlplane

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
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

func newNodeControlClientProvider(target, caPath, certPath, keyPath string) (NodeControlClientProvider, error) {
	opts, err := nodeControlDialOptions(caPath, certPath, keyPath)
	if err != nil {
		return nil, err
	}
	return &nodeControlClientProvider{target: strings.TrimSpace(target), opts: opts}, nil
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

func nodeControlDialOptions(caPath, certPath, keyPath string) ([]grpc.DialOption, error) {
	caPath = strings.TrimSpace(caPath)
	certPath = strings.TrimSpace(certPath)
	keyPath = strings.TrimSpace(keyPath)
	var opts []grpc.DialOption
	if caPath == "" && certPath == "" && keyPath == "" {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		if caPath == "" || certPath == "" || keyPath == "" {
			return nil, fmt.Errorf("control-plane mTLS requires ca cert, client cert, and client key")
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, err
		}
		caPEM, err := os.ReadFile(caPath)
		if err != nil {
			return nil, err
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse control-plane tls ca cert %q", caPath)
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion:   tls.VersionTLS12,
			RootCAs:      roots,
			Certificates: []tls.Certificate{cert},
		})))
	}
	opts = append(opts, sdkobs.GRPCDialOptions()...)
	opts = append(opts, grpc.WithNoProxy())
	return opts, nil
}
