package tunnelrelay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"strings"

	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
	"github.com/cofy-x/axern/apps/cli/internal/controlv1"
	"github.com/cofy-x/axern/lib/go/grpcclient"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func Config(config controlv1.Config) apptunnel.RelayDialConfig {
	return apptunnel.RelayDialConfig{CACert: config.TLSCACert, Cert: config.TLSCert, Key: config.TLSKey, ServerName: config.TLSServerName, ProxyMode: config.ProxyMode}
}

func PeerDialer(ctx context.Context, target string, cfg apptunnel.RelayDialConfig) (apptunnel.RelayPeerStream, io.Closer, error) {
	dialOpts, err := DialOptions(cfg)
	if err != nil {
		return nil, nil, err
	}
	conn, err := grpcclient.NewReadyClient(ctx, target, dialOpts...)
	if err != nil {
		return nil, nil, err
	}
	stream, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return stream, conn, nil
}

func DialOptions(cfg apptunnel.RelayDialConfig) ([]grpc.DialOption, error) {
	proxyOpt, err := proxyDialOption(cfg.ProxyMode)
	if err != nil {
		return nil, err
	}
	if cfg.Insecure {
		opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
		if proxyOpt != nil {
			opts = append(opts, proxyOpt)
		}
		return opts, nil
	}
	if strings.TrimSpace(cfg.CACert) == "" {
		return nil, fmt.Errorf("tunnel relay TLS requires the context TLS CA certificate")
	}
	var certificates []tls.Certificate
	if strings.TrimSpace(cfg.Cert) != "" || strings.TrimSpace(cfg.Key) != "" {
		if strings.TrimSpace(cfg.Cert) == "" || strings.TrimSpace(cfg.Key) == "" {
			return nil, fmt.Errorf("tunnel relay mTLS requires both tls cert and tls key")
		}
		cert, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
		if err != nil {
			return nil, err
		}
		certificates = []tls.Certificate{cert}
	}
	caPEM, err := os.ReadFile(cfg.CACert)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse relay tls ca cert %q", cfg.CACert)
	}
	opts := []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      roots,
		Certificates: certificates,
		ServerName:   strings.TrimSpace(cfg.ServerName),
	}))}
	if proxyOpt != nil {
		opts = append(opts, proxyOpt)
	}
	return opts, nil
}

func proxyDialOption(mode string) (grpc.DialOption, error) {
	switch strings.TrimSpace(mode) {
	case "", controlv1.ProxyModeEnv:
		return nil, nil
	case controlv1.ProxyModeDirect:
		return grpc.WithNoProxy(), nil
	default:
		return nil, fmt.Errorf("invalid proxy mode %q; expected %q or %q", mode, controlv1.ProxyModeEnv, controlv1.ProxyModeDirect)
	}
}
