package control

import (
	"context"
	"time"

	tunnelrelaycontrolv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/tunnel/v1"
	"github.com/cofy-x/axern/lib/go/grpcclient"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"google.golang.org/grpc"
)

func Dial(ctx context.Context, target, trustPath, bundlePath, cluster string) (*grpc.ClientConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return grpcclient.NewReadyClient(dialCtx, target, grpc.WithTransportCredentials(&workloadtls.Credentials{BundlePath: bundlePath, TrustPath: trustPath, Local: workloadtls.Identity{Cluster: cluster, Role: "tunneld"}, Peer: workloadtls.Identity{Cluster: cluster, Role: "controld"}}), grpc.WithNoProxy())
}

func NewRelayControlClient(conn *grpc.ClientConn) tunnelrelaycontrolv1.TunnelRelayControlClient {
	return tunnelrelaycontrolv1.NewTunnelRelayControlClient(conn)
}
