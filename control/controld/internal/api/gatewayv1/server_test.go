package gatewayv1

import (
	"context"
	"errors"
	"testing"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	gatewaypb "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/gateway/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type resolverStub struct {
	terminalCalls int
}

func (r *resolverStub) ValidateAllocationAccess(context.Context, *gatewaypb.ResolveAllocationTerminalRequest, time.Time) error {
	return nil
}

func (r *resolverStub) ResolveAllocationTerminal(context.Context, *gatewaypb.ResolveAllocationTerminalRequest, time.Duration, time.Time) (*gatewaypb.ResolveAllocationTerminalResponse, error) {
	r.terminalCalls++
	return &gatewaypb.ResolveAllocationTerminalResponse{}, nil
}

type accessAuthorizerStub struct {
	calls       int
	fingerprint string
	action      accesskernel.Action
	resource    string
	resourceID  string
	err         error
}

type tunnelResolverStub struct {
	sessionID string
	target    string
	err       error
}

func (r *tunnelResolverStub) ResolveRelayTarget(_ context.Context, sessionID string, _ time.Time) (string, error) {
	r.sessionID = sessionID
	return r.target, r.err
}

func (a *accessAuthorizerStub) AuthorizeCredentialResource(_ context.Context, fingerprint string, _ accesskernel.CredentialKind, action accesskernel.Action, resource, resourceID string) error {
	a.calls++
	a.fingerprint = fingerprint
	a.action = action
	a.resource = resource
	a.resourceID = resourceID
	return a.err
}

func TestResolveAllocationTerminalRejectsMissingPrincipal(t *testing.T) {
	resolver := &resolverStub{}
	access := &accessAuthorizerStub{}
	server := New(Dependencies{Resolver: resolver, Access: access, DefaultTTL: time.Minute})

	_, err := server.ResolveAllocationTerminal(context.Background(), &gatewaypb.ResolveAllocationTerminalRequest{AllocationID: "alloc-1"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing Principal accepted: %v", err)
	}
	if access.calls != 0 {
		t.Fatalf("authorization calls = %d, want 0 for missing identity", access.calls)
	}
	if resolver.terminalCalls != 0 {
		t.Fatalf("unauthenticated resolution calls = %d", resolver.terminalCalls)
	}
}

func TestResolveAllocationTerminalPrincipalIdentity(t *testing.T) {
	resolver := &resolverStub{}
	access := &accessAuthorizerStub{}
	server := New(Dependencies{Resolver: resolver, Access: access, DefaultTTL: time.Minute})
	req := &gatewaypb.ResolveAllocationTerminalRequest{
		AllocationID:          "alloc-1",
		CredentialFingerprint: "sha256:client",
	}

	_, err := server.ResolveAllocationTerminal(context.Background(), req)
	if err != nil {
		t.Fatalf("ResolveAllocationTerminal() error = %v", err)
	}
	if access.calls != 1 || access.fingerprint != req.CredentialFingerprint {
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
		{name: "permission denied", err: accesskernel.ErrPermissionDenied, code: codes.NotFound},
		{name: "not found", err: accesskernel.ErrNotFound, code: codes.NotFound},
		{name: "internal", err: errors.New("database unavailable"), code: codes.Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &resolverStub{}
			access := &accessAuthorizerStub{err: tt.err}
			server := New(Dependencies{Resolver: resolver, Access: access})

			_, err := server.ResolveAllocationTerminal(context.Background(), &gatewaypb.ResolveAllocationTerminalRequest{
				AllocationID:          "alloc-1",
				CredentialFingerprint: "sha256:client",
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
	tunnels := &tunnelResolverStub{target: "tunneld:24100"}
	server := New(Dependencies{Resolver: &resolverStub{}, Tunnels: tunnels})

	response, err := server.ResolveTunnelRelayTarget(context.Background(), &gatewaypb.ResolveTunnelRelayTargetRequest{SessionID: "tun-1"})
	if err != nil {
		t.Fatalf("ResolveTunnelRelayTarget() error = %v", err)
	}
	if tunnels.sessionID != "tun-1" || response.GetNodeEdgeTarget() != "tunneld:24100" {
		t.Fatalf("session = %q, response = %#v", tunnels.sessionID, response)
	}
}
