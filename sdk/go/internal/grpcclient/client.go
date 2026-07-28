package grpcclient

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// PassthroughTarget preserves the target string for custom dialers.
func PassthroughTarget(target string) string {
	if strings.Contains(target, ":///") {
		return target
	}
	return "passthrough:///" + target
}

// NewReadyClient creates a gRPC ClientConn and waits for it to reach Ready.
func NewReadyClient(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, err
	}
	if err := WaitReady(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// WaitReady exits idle mode and waits until conn reaches Ready or ctx expires.
func WaitReady(ctx context.Context, conn *grpc.ClientConn) error {
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Idle {
			conn.Connect()
		}
		if !conn.WaitForStateChange(ctx, state) {
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("gRPC client connection did not become ready")
		}
	}
}
