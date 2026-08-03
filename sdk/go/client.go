package axernsdk

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	artifactv1 "github.com/cofy-x/axern/sdk/go/gen/axern/data/artifact/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"github.com/cofy-x/axern/sdk/go/internal/grpcclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is the root Axern Go SDK client.
type Client struct {
	conn          *grpc.ClientConn
	ownsConn      bool
	dialOptions   []grpc.DialOption
	relayOptions  relayOptions
	environments  environmentv1.EnvironmentControlClient
	agentProfiles agentprofilev1.AgentProfileControlClient
	rollouts      rolloutv1.RolloutControlClient
	runs          runv1.RunControlClient
	services      servicev1.ServiceControlClient
	tunnels       tunnelcontrolv1.TunnelControlClient
	nodes         nodesandboxv1.NodeSandboxClient
	artifacts     artifactv1.ArtifactDataClient
}

// ClientOption configures a Client.
type ClientOption func(*clientConfig) error

type clientConfig struct {
	conn                 *grpc.ClientConn
	dialOptions          []grpc.DialOption
	relayOptions         relayOptions
	customDialOptions    bool
	transportCredentials bool
}

type relayOptions struct {
	Insecure   bool
	TLSCACert  string
	TLSCert    string
	TLSKey     string
	ServerName string
	ProxyMode  string
}

const (
	// ProxyModeEnv uses gRPC's environment proxy configuration.
	ProxyModeEnv = "env"
	// ProxyModeDirect bypasses environment proxies for all SDK connections.
	ProxyModeDirect = "direct"
)

// WithControlConn uses an existing control-plane gRPC connection.
func WithControlConn(conn *grpc.ClientConn) ClientOption {
	return func(config *clientConfig) error {
		config.conn = conn
		return nil
	}
}

// WithDialOptions appends gRPC dial options for the control-plane connection.
func WithDialOptions(options ...grpc.DialOption) ClientOption {
	return func(config *clientConfig) error {
		config.dialOptions = append(config.dialOptions, options...)
		config.customDialOptions = true
		return nil
	}
}

// WithProxyMode configures proxy handling for both control-plane and tunnel relay connections.
func WithProxyMode(mode string) ClientOption {
	return func(config *clientConfig) error {
		mode = strings.ToLower(strings.TrimSpace(mode))
		switch mode {
		case "", ProxyModeEnv:
			mode = ProxyModeEnv
		case ProxyModeDirect:
		default:
			return fmt.Errorf("proxy mode must be %q or %q", ProxyModeEnv, ProxyModeDirect)
		}
		config.relayOptions.ProxyMode = mode
		return nil
	}
}

// WithTLS configures mutual TLS for the control-plane connection.
func WithTLS(caCertPath, certPath, keyPath, serverName string) ClientOption {
	return func(config *clientConfig) error {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: serverName,
		}
		if caCertPath != "" {
			pem, err := os.ReadFile(caCertPath)
			if err != nil {
				return err
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return fmt.Errorf("failed to load CA certificate %s", caCertPath)
			}
			tlsConfig.RootCAs = pool
		}
		if certPath != "" || keyPath != "" {
			cert, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err != nil {
				return err
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
		config.dialOptions = append(config.dialOptions, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
		config.transportCredentials = true
		config.relayOptions.TLSCACert = caCertPath
		config.relayOptions.TLSCert = certPath
		config.relayOptions.TLSKey = keyPath
		config.relayOptions.ServerName = serverName
		return nil
	}
}

// NewClient connects to the Axern control plane at target.
func NewClient(ctx context.Context, target string, options ...ClientOption) (*Client, error) {
	config := clientConfig{}
	for _, option := range options {
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	if config.relayOptions.ProxyMode == ProxyModeDirect {
		config.dialOptions = append(config.dialOptions, grpc.WithNoProxy())
	}
	if !config.transportCredentials && !config.customDialOptions {
		config.dialOptions = append(config.dialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
		config.relayOptions.Insecure = true
	}
	conn := config.conn
	ownsConn := false
	if conn == nil {
		var err error
		conn, err = grpcclient.NewReadyClient(ctx, grpcclient.PassthroughTarget(target), config.dialOptions...)
		if err != nil {
			return nil, err
		}
		ownsConn = true
	}
	return &Client{
		conn:          conn,
		ownsConn:      ownsConn,
		dialOptions:   append([]grpc.DialOption(nil), config.dialOptions...),
		relayOptions:  config.relayOptions,
		environments:  environmentv1.NewEnvironmentControlClient(conn),
		agentProfiles: agentprofilev1.NewAgentProfileControlClient(conn),
		rollouts:      rolloutv1.NewRolloutControlClient(conn),
		runs:          runv1.NewRunControlClient(conn),
		services:      servicev1.NewServiceControlClient(conn),
		tunnels:       tunnelcontrolv1.NewTunnelControlClient(conn),
		nodes:         nodesandboxv1.NewNodeSandboxClient(conn),
		artifacts:     artifactv1.NewArtifactDataClient(conn),
	}, nil
}

// Close closes the control-plane connection when the client owns it.
func (c *Client) Close() error {
	if c == nil || !c.ownsConn || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func countNonEmpty(values ...string) int {
	count := 0
	for _, value := range values {
		if value != "" {
			count++
		}
	}
	return count
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func cloneMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
