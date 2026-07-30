package gatewayv1

import (
	"context"
	"errors"
	"testing"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	gatewaypb "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	tunnelpb "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type resolverStub struct {
	terminalCalls int
}

func (*resolverStub) ResolveServiceRoute(context.Context, *gatewaypb.ResolveServiceRouteRequest, time.Duration, time.Time) (*gatewaypb.ResolveServiceRouteResponse, error) {
	return &gatewaypb.ResolveServiceRouteResponse{}, nil
}

func (r *resolverStub) ResolveAllocationTerminal(context.Context, *gatewaypb.ResolveAllocationTerminalRequest, time.Duration, time.Time) (*gatewaypb.ResolveAllocationTerminalResponse, error) {
	r.terminalCalls++
	return &gatewaypb.ResolveAllocationTerminalResponse{}, nil
}

func (*resolverStub) ResolveServiceReplicaTargets(context.Context, string) (*gatewaypb.ResolveServiceReplicaTargetsResponse, error) {
	return &gatewaypb.ResolveServiceReplicaTargetsResponse{}, nil
}

type accessAuthorizerStub struct {
	calls       int
	fingerprint string
	lease       string
	action      accesskernel.Action
	resource    string
	resourceID  string
	err         error
}

type tunnelResolverStub struct {
	sessionID string
	session   *tunnelpb.TunnelSession
	err       error
}

func (r *tunnelResolverStub) Get(_ context.Context, sessionID string, _ time.Time) (*tunnelpb.TunnelSession, error) {
	r.sessionID = sessionID
	return r.session, r.err
}

func (a *accessAuthorizerStub) AuthorizeFingerprintResource(_ context.Context, fingerprint, lease string, action accesskernel.Action, resource, resourceID string) error {
	a.calls++
	a.fingerprint = fingerprint
	a.lease = lease
	a.action = action
	a.resource = resource
	a.resourceID = resourceID
	return a.err
}

func TestResolveAllocationTerminalGatewayOwnedIdentity(t *testing.T) {
	resolver := &resolverStub{}
	access := &accessAuthorizerStub{}
	server := New(Dependencies{Resolver: resolver, Access: access, DefaultTTL: time.Minute})

	_, err := server.ResolveAllocationTerminal(context.Background(), &gatewaypb.ResolveAllocationTerminalRequest{AllocationID: "alloc-1"})
	if err != nil {
		t.Fatalf("ResolveAllocationTerminal() error = %v", err)
	}
	if access.calls != 0 {
		t.Fatalf("authorization calls = %d, want 0 for gateway-owned HTTP/SSH identity", access.calls)
	}
	if resolver.terminalCalls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.terminalCalls)
	}
}

func TestResolveAllocationTerminalPrincipalIdentity(t *testing.T) {
	resolver := &resolverStub{}
	access := &accessAuthorizerStub{}
	server := New(Dependencies{Resolver: resolver, Access: access, DefaultTTL: time.Minute})
	req := &gatewaypb.ResolveAllocationTerminalRequest{
		AllocationID:                 "alloc-1",
		ClientCertificateFingerprint: "sha256:client",
		RolloutExecutionLease:        "lease-1",
	}

	_, err := server.ResolveAllocationTerminal(context.Background(), req)
	if err != nil {
		t.Fatalf("ResolveAllocationTerminal() error = %v", err)
	}
	if access.calls != 1 || access.fingerprint != req.ClientCertificateFingerprint || access.lease != req.RolloutExecutionLease {
		t.Fatalf("authorization call = %#v", access)
	}
	if access.action != accesskernel.ActionSandboxExecute || access.resource != "allocation" || access.resourceID != req.AllocationID {
		t.Fatalf("authorization scope = (%q, %q, %q)", access.action, access.resource, access.resourceID)
	}
	if resolver.terminalCalls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.terminalCalls)
	}
}

func TestResolveAllocationTerminalAuthorizationErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "unauthenticated", err: accesskernel.ErrUnauthenticated, code: codes.Unauthenticated},
		{name: "permission denied", err: accesskernel.ErrPermissionDenied, code: codes.PermissionDenied},
		{name: "not found", err: accesskernel.ErrNotFound, code: codes.NotFound},
		{name: "internal", err: errors.New("database unavailable"), code: codes.Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &resolverStub{}
			access := &accessAuthorizerStub{err: tt.err}
			server := New(Dependencies{Resolver: resolver, Access: access})

			_, err := server.ResolveAllocationTerminal(context.Background(), &gatewaypb.ResolveAllocationTerminalRequest{
				AllocationID:                 "alloc-1",
				ClientCertificateFingerprint: "sha256:client",
			})
			if status.Code(err) != tt.code {
				t.Fatalf("error code = %s, want %s (err = %v)", status.Code(err), tt.code, err)
			}
			if resolver.terminalCalls != 0 {
				t.Fatalf("resolver calls = %d, want 0", resolver.terminalCalls)
			}
		})
	}
}

func TestResolveTunnelRelayTargetUsesPrivateSessionState(t *testing.T) {
	tunnels := &tunnelResolverStub{session: &tunnelpb.TunnelSession{NodeEdgeTarget: "tunneld:24100"}}
	server := New(Dependencies{Resolver: &resolverStub{}, Tunnels: tunnels})

	response, err := server.ResolveTunnelRelayTarget(context.Background(), &gatewaypb.ResolveTunnelRelayTargetRequest{SessionID: "tun-1"})
	if err != nil {
		t.Fatalf("ResolveTunnelRelayTarget() error = %v", err)
	}
	if tunnels.sessionID != "tun-1" || response.GetNodeEdgeTarget() != "tunneld:24100" {
		t.Fatalf("session = %q, response = %#v", tunnels.sessionID, response)
	}
}
