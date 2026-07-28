package rpcstatus

import (
	"context"
	"errors"
	"io"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type DependencyErrorClassifier func(error) bool

func UnaryServerInterceptor(classify DependencyErrorClassifier) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		resp, err := handler(ctx, req)
		return resp, normalize(err, classify)
	}
}

func StreamServerInterceptor(classify DependencyErrorClassifier) grpc.StreamServerInterceptor {
	return func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		return normalize(handler(srv, stream), classify)
	}
}

func normalize(err error, classify DependencyErrorClassifier) error {
	if err == nil || grpcstatus.Code(err) != codes.Unknown || !dependencyUnavailable(err, classify) {
		return err
	}
	return grpcstatus.Error(codes.Unavailable, "control-plane dependency is unavailable")
}

func dependencyUnavailable(err error, classify DependencyErrorClassifier) bool {
	if classify != nil && classify(err) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
