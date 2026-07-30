package controlv1

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const dialTimeout = 15 * time.Second

func dial(ctx context.Context, config Config) (*grpc.ClientConn, Clients, error) {
	conn, err := dialControlGRPC(ctx, config)
	if err != nil {
		return nil, Clients{}, err
	}
	return conn, Clients{
		Admin:            adminv1.NewAllocationLifecycleAdminClient(conn),
		AdminAudit:       adminv1.NewAdminAuditClient(conn),
		AdminReliability: adminv1.NewAdminReliabilityClient(conn),
		AdminStorage:     adminv1.NewStorageAdminClient(conn),
		AdminService:     adminv1.NewServiceAdminClient(conn),
		AdminNode:        adminv1.NewNodeAdminClient(conn),
		AccessAdmin:      adminv1.NewAccessAdminClient(conn),
		Identity:         identityv1.NewIdentityControlClient(conn),
		Environment:      environmentv1.NewEnvironmentControlClient(conn),
		Function:         functionv1.NewFunctionControlClient(conn),
		Run:              runv1.NewRunControlClient(conn),
		Secret:           secretv1.NewSecretControlClient(conn),
		Service:          servicev1.NewServiceControlClient(conn),
		Catalog:          catalogv1.NewRuntimeCatalogClient(conn),
		Tunnel:           tunnelv1.NewTunnelControlClient(conn),
		Namespace:        namespacev1.NewNamespaceControlClient(conn),
		Quota:            quotav1.NewQuotaControlClient(conn),
	}, nil
}

func dialControlGRPC(ctx context.Context, config Config) (*grpc.ClientConn, error) {
	target := config.Endpoint
	caPath := config.TLSCACert
	certPath := config.TLSCert
	keyPath := config.TLSKey
	if caPath == "" && certPath == "" && keyPath == "" {
		return nil, fmt.Errorf("gateway mTLS requires --tls-ca-cert, --tls-cert, and --tls-key")
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
		return nil, fmt.Errorf("parse tls ca cert %q", caPath)
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion:   tls.VersionTLS12,
			RootCAs:      roots,
			Certificates: []tls.Certificate{cert},
			ServerName:   config.TLSServerName,
		})),
	}
	switch config.ProxyMode {
	case "", ProxyModeEnv:
	case ProxyModeDirect:
		dialOpts = append(dialOpts, grpc.WithNoProxy())
	default:
		return nil, fmt.Errorf("invalid proxy mode %q; expected %q or %q", config.ProxyMode, ProxyModeEnv, ProxyModeDirect)
	}
	return grpcclient.NewReadyClient(
		dialCtx,
		target,
		dialOpts...,
	)
}
