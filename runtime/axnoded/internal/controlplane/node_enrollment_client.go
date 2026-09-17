package controlplane

import (
	"context"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/lib/go/grpcclient"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// enrollmentClient uses a new bounded transport per issuance. Bootstrap never
// supplies a Node key; renewal always proves possession of the published key.
// Rotation therefore cannot silently keep using an old cached TLS connection.
type enrollmentClient struct {
	target, trustPath, bundlePath string
	identity                      workloadtls.Identity
}

func NewNodeEnrollmentClient(target, trustPath, bundlePath string, identity workloadtls.Identity) nodev1.NodeEnrollmentClient {
	return &enrollmentClient{target: target, trustPath: trustPath, bundlePath: bundlePath, identity: identity}
}

func (c *enrollmentClient) dial(ctx context.Context, transport credentials.TransportCredentials) (*grpc.ClientConn, error) {
	return grpcclient.NewReadyClient(ctx, c.target, grpc.WithTransportCredentials(transport), grpc.WithNoProxy())
}

func (c *enrollmentClient) EnrollNode(ctx context.Context, req *nodev1.EnrollNodeRequest, opts ...grpc.CallOption) (*nodev1.EnrollNodeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := c.dial(ctx, &workloadtls.BootstrapCredentials{TrustPath: c.trustPath, Peer: workloadtls.Identity{Cluster: c.identity.Cluster, Role: "controld"}})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return nodev1.NewNodeEnrollmentClient(conn).EnrollNode(ctx, req, opts...)
}

func (c *enrollmentClient) RenewNodeCertificate(ctx context.Context, req *nodev1.RenewNodeCertificateRequest, opts ...grpc.CallOption) (*nodev1.RenewNodeCertificateResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := c.dial(ctx, &workloadtls.Credentials{BundlePath: c.bundlePath, TrustPath: c.trustPath, Local: c.identity, Peer: workloadtls.Identity{Cluster: c.identity.Cluster, Role: "controld"}})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return nodev1.NewNodeEnrollmentClient(conn).RenewNodeCertificate(ctx, req, opts...)
}
