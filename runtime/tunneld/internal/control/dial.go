package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type TLSConfig struct {
	CACert string
	Cert   string
	Key    string
}

func Dial(ctx context.Context, target string, tlsCfg TLSConfig, insecureTransport bool) (*grpc.ClientConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var opts []grpc.DialOption
	if insecureTransport {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		creds, err := loadTLS(tlsCfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	}
	return grpcclient.NewReadyClient(dialCtx, target, opts...)
}

func NewRelayControlClient(conn *grpc.ClientConn) tunnelrelaycontrolv1.TunnelRelayControlClient {
	return tunnelrelaycontrolv1.NewTunnelRelayControlClient(conn)
}

func loadTLS(cfg TLSConfig) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if err != nil {
		return nil, err
	}
	caPEM, err := os.ReadFile(cfg.CACert)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse tls ca cert %q", cfg.CACert)
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      roots,
		Certificates: []tls.Certificate{cert},
	}), nil
}
