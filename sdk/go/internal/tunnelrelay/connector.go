package tunnelrelay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"github.com/cofy-x/axern/sdk/go/internal/grpcclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type ConnectorConfig struct {
	SessionID        string
	EdgeTarget       string
	ClientEdgeTarget string
	ClientToken      string
	LocalTarget      string
	RelayInsecure    bool
	RelayTLSCACert   string
	RelayTLSCert     string
	RelayTLSKey      string
	RelayServerName  string
	ProxyMode        string
	PingInterval     time.Duration
	DialTimeout      time.Duration
	MaxStreams       int
}

func RunConnector(ctx context.Context, config ConnectorConfig) error {
	if err := validateProxyMode(config.ProxyMode); err != nil {
		return err
	}
	target := config.ClientEdgeTarget
	if target == "" {
		target = config.EdgeTarget
	}
	if target == "" {
		return fmt.Errorf("tunnel session %s has no relay target", config.SessionID)
	}
	backoff := time.Second
	for {
		err := runOnce(ctx, target, config)
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		if isTerminalRelayError(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 10*time.Second {
			backoff *= 2
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}
		}
	}
}

func runOnce(ctx context.Context, target string, config ConnectorConfig) error {
	conn, err := dialRelay(ctx, target, config)
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{
		SessionID: config.SessionID,
		PeerKind:  tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		Token:     config.ClientToken,
	}}}); err != nil {
		return err
	}
	return newSession(stream, config.LocalTarget, sessionConfig{
		PingInterval: config.PingInterval,
		DialTimeout:  config.DialTimeout,
		MaxStreams:   config.MaxStreams,
	}).run(ctx)
}

func dialRelay(ctx context.Context, target string, config ConnectorConfig) (*grpc.ClientConn, error) {
	var dialOptions []grpc.DialOption
	switch config.ProxyMode {
	case "", "env":
	case "direct":
		dialOptions = append(dialOptions, grpc.WithNoProxy())
	default:
		return nil, validateProxyMode(config.ProxyMode)
	}
	if config.RelayInsecure {
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.RelayServerName}
		if config.RelayTLSCACert != "" {
			pem, err := os.ReadFile(config.RelayTLSCACert)
			if err != nil {
				return nil, err
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("failed to load relay CA certificate %s", config.RelayTLSCACert)
			}
			tlsConfig.RootCAs = pool
		}
		if config.RelayTLSCert != "" || config.RelayTLSKey != "" {
			cert, err := tls.LoadX509KeyPair(config.RelayTLSCert, config.RelayTLSKey)
			if err != nil {
				return nil, err
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}
	return grpcclient.NewReadyClient(ctx, grpcclient.PassthroughTarget(target), dialOptions...)
}

func validateProxyMode(mode string) error {
	switch mode {
	case "", "env", "direct":
		return nil
	default:
		return fmt.Errorf("proxy mode must be %q or %q", "env", "direct")
	}
}

func isTerminalRelayError(err error) bool {
	code := status.Code(err)
	return code == codes.PermissionDenied || code == codes.Unauthenticated || code == codes.NotFound
}
