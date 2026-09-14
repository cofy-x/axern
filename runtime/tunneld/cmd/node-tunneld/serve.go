package main

import (
	"context"
	"fmt"
	"net"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (d *daemon) serveSession(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token, nodeEdgeTarget string) error {
	network, err := d.operator.ResolveSandboxNetwork(ctx, &nodeoperatorv1.ResolveSandboxNetworkRequest{SandboxID: session.GetAllocationID()})
	if err != nil {
		switch grpcstatus.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
			return degradedSessionError(err)
		}
		return err
	}
	return d.serveRunscSession(ctx, session, token, nodeEdgeTarget, network)
}

func resolveSandboxReachableTarget(target string) (string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return "", err
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			return net.JoinHostPort(ip4.String(), port), nil
		}
	}
	if len(ips) > 0 {
		return net.JoinHostPort(ips[0].String(), port), nil
	}
	return "", fmt.Errorf("resolve %q: no addresses", host)
}

func serverNameFromTarget(target string) (string, error) {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return "", err
	}
	return host, nil
}
