package main

import (
	"context"
	"net"
	"strings"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func dialUnix(ctx context.Context, path string) (*grpc.ClientConn, error) {
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", path)
	}
	return grpcclient.NewReadyClient(ctx, "unix:"+path, grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
