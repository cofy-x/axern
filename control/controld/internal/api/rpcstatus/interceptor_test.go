package rpcstatus

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestNormalizeMapsDependencyConnectivityWithoutLeakingDetails(t *testing.T) {
	dependencyErr := errors.New("database is starting")
	err := fmt.Errorf("query service: %w", dependencyErr)

	got := normalize(err, func(err error) bool { return errors.Is(err, dependencyErr) })

	if grpcstatus.Code(got) != codes.Unavailable {
		t.Fatalf("normalize() code = %s, want Unavailable", grpcstatus.Code(got))
	}
	if got.Error() != "rpc error: code = Unavailable desc = control-plane dependency is unavailable" {
		t.Fatalf("normalize() error = %q", got)
	}
}

func TestNormalizeMapsWrappedNetworkErrors(t *testing.T) {
	err := fmt.Errorf("connect postgres: %w", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")})
	if got := grpcstatus.Code(normalize(err, nil)); got != codes.Unavailable {
		t.Fatalf("normalize() code = %s, want Unavailable", got)
	}
}

func TestNormalizePreservesDomainAndUnknownErrors(t *testing.T) {
	domainErr := grpcstatus.Error(codes.NotFound, "service not found")
	if got := normalize(domainErr, nil); !errors.Is(got, domainErr) {
		t.Fatalf("normalize() = %v, want original domain error", got)
	}
	unknownErr := errors.New("broken invariant")
	if got := normalize(unknownErr, nil); !errors.Is(got, unknownErr) {
		t.Fatalf("normalize() = %v, want original unknown error", got)
	}
}

func TestUnaryServerInterceptorNormalizesHandlerError(t *testing.T) {
	dependencyErr := errors.New("connection failure")
	interceptor := UnaryServerInterceptor(func(err error) bool { return errors.Is(err, dependencyErr) })
	_, err := interceptor(context.Background(), nil, nil, func(context.Context, any) (any, error) {
		return nil, dependencyErr
	})
	if got := grpcstatus.Code(err); got != codes.Unavailable {
		t.Fatalf("interceptor code = %s, want Unavailable", got)
	}
}
