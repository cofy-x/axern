package authz

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"sync"
	"testing"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type fakeAccess struct {
	actor     accesskernel.Actor
	namespace string
	leaseErr  error
}

func (f fakeAccess) ResolveActor(context.Context, [32]byte) (accesskernel.Actor, error) {
	return f.actor, nil
}
func (f fakeAccess) ResolveResourceNamespace(context.Context, string, string) (string, error) {
	return f.namespace, nil
}
func (f fakeAccess) ValidateRolloutExecutionLease(context.Context, string, string) error {
	return f.leaseErr
}

func TestUnaryRejectsSpoofedOrUnauthorizedIdentity(t *testing.T) {
	actor := accesskernel.Actor{Principal: accesskernel.Principal{Status: accesskernel.PrincipalStatusActive}, Bindings: []accesskernel.Binding{{Role: accesskernel.RoleNamespaceViewer, Namespace: "team-a"}}}
	i := &Interceptor{access: fakeAccess{actor: actor, namespace: "team-a"}, gatewayPeer: func(context.Context) bool { return true }}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(ClientCertificateFingerprintMetadata, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		if _, ok := accesskernel.ActorFromContext(ctx); !ok {
			t.Fatal("actor missing from handler context")
		}
		return req, nil
	}
	_, err := i.Unary(ctx, &servicev1.GetServiceRequest{ServiceID: "svc-a"}, &grpc.UnaryServerInfo{FullMethod: "/axern.control.service.v1.ServiceControl/GetService"}, handler)
	if err != nil || !called {
		t.Fatalf("authorized read failed: called=%v err=%v", called, err)
	}
	called = false
	_, err = i.Unary(ctx, &servicev1.DeleteServiceRequest{ServiceID: "svc-a"}, &grpc.UnaryServerInfo{FullMethod: "/axern.control.service.v1.ServiceControl/DeleteService"}, handler)
	if status.Code(err) != codes.PermissionDenied || called {
		t.Fatalf("write result: called=%v code=%v", called, status.Code(err))
	}
	i.gatewayPeer = func(context.Context) bool { return false }
	_, err = i.Unary(ctx, &servicev1.GetServiceRequest{ServiceID: "svc-a"}, &grpc.UnaryServerInfo{FullMethod: "/axern.control.service.v1.ServiceControl/GetService"}, handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("direct peer code=%v", status.Code(err))
	}
}

func TestUnaryUsesScopedRolloutExecutionLease(t *testing.T) {
	actor := accesskernel.Actor{Principal: accesskernel.Principal{Status: accesskernel.PrincipalStatusActive}, Bindings: []accesskernel.Binding{{Role: accesskernel.RoleRolloutExecutor}}}
	i := &Interceptor{access: fakeAccess{actor: actor, namespace: "team-a"}, gatewayPeer: func(context.Context) bool { return true }}
	base := metadata.Pairs(
		ClientCertificateFingerprintMetadata, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RolloutExecutionLeaseMetadata, "lease",
	)
	ctx := metadata.NewIncomingContext(context.Background(), base)
	called := false
	_, err := i.Unary(ctx, &servicev1.DeleteServiceRequest{ServiceID: "svc-a"}, &grpc.UnaryServerInfo{FullMethod: "/axern.control.service.v1.ServiceControl/DeleteService"}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if err != nil || !called {
		t.Fatalf("delegated write called=%v err=%v", called, err)
	}
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs(ClientCertificateFingerprintMetadata, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	_, err = i.Unary(ctx, &servicev1.DeleteServiceRequest{ServiceID: "svc-a"}, &grpc.UnaryServerInfo{FullMethod: "/axern.control.service.v1.ServiceControl/DeleteService"}, func(context.Context, any) (any, error) { return nil, nil })
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("missing delegation code=%v", status.Code(err))
	}
}

func TestRolloutExecutionLeaseCannotManageQuota(t *testing.T) {
	actor := accesskernel.Actor{Principal: accesskernel.Principal{Status: accesskernel.PrincipalStatusActive}, Bindings: []accesskernel.Binding{{Role: accesskernel.RoleRolloutExecutor}}}
	i := &Interceptor{access: fakeAccess{actor: actor}, gatewayPeer: func(context.Context) bool { return true }}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		ClientCertificateFingerprintMetadata, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RolloutExecutionLeaseMetadata, "lease",
	))
	called := false
	_, err := i.Unary(ctx, &quotav1.SetNamespaceQuotaRequest{Namespace: "team-a"}, &grpc.UnaryServerInfo{FullMethod: "/axern.control.quota.v1.QuotaControl/SetNamespaceQuota"}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if status.Code(err) != codes.PermissionDenied || called {
		t.Fatalf("quota management called=%v code=%v", called, status.Code(err))
	}
}

func TestUnaryHidesCrossNamespaceResource(t *testing.T) {
	actor := accesskernel.Actor{Principal: accesskernel.Principal{Status: accesskernel.PrincipalStatusActive}, Bindings: []accesskernel.Binding{{Role: accesskernel.RoleNamespaceViewer, Namespace: "team-a"}}}
	i := &Interceptor{access: fakeAccess{actor: actor, namespace: "team-b"}, gatewayPeer: func(context.Context) bool { return true }}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(ClientCertificateFingerprintMetadata, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	_, err := i.Unary(ctx, &servicev1.GetServiceRequest{ServiceID: "svc-b"}, &grpc.UnaryServerInfo{FullMethod: "/axern.control.service.v1.ServiceControl/GetService"}, func(context.Context, any) (any, error) { return nil, nil })
	if status.Code(err) != codes.NotFound {
		t.Fatalf("cross-namespace lookup code=%v", status.Code(err))
	}
}

func TestFieldSearchTraversesRepeatedMessages(t *testing.T) {
	request := &servicev1.UpdateServiceRequest{
		ServiceID: "svc-a",
		Config: &commonv1.ExecutionConfig{
			Ports: []*commonv1.PortSpec{{}},
		},
	}
	if got := findStringField(request.ProtoReflect(), "namespace"); got != "" {
		t.Fatalf("namespace = %q, want empty", got)
	}
	if got := findStringField(request.ProtoReflect(), "service_id"); got != "svc-a" {
		t.Fatalf("service_id = %q, want svc-a", got)
	}
}

type changingAccess struct {
	mu    sync.Mutex
	actor accesskernel.Actor
}

func (f *changingAccess) ResolveActor(context.Context, [32]byte) (accesskernel.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.actor, nil
}
func (*changingAccess) ResolveResourceNamespace(context.Context, string, string) (string, error) {
	return "team-a", nil
}
func (*changingAccess) ValidateRolloutExecutionLease(context.Context, string, string) error {
	return nil
}

func TestStreamRecheckCancelsAfterRoleRevocation(t *testing.T) {
	access := &changingAccess{actor: accesskernel.Actor{Principal: accesskernel.Principal{Status: accesskernel.PrincipalStatusActive}, Bindings: []accesskernel.Binding{{Role: accesskernel.RoleNamespaceViewer, Namespace: "team-a"}}}}
	i := &Interceptor{access: access, recheckInterval: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	stream := &actorServerStream{ctx: ctx, cancel: cancel, interceptor: i, actor: access.actor, policy: methodPolicy{action: accesskernel.ActionResourceRead}}
	go stream.recheck("team-a")
	access.mu.Lock()
	access.actor.Bindings = nil
	access.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stream context was not cancelled after role revocation")
	}
}

func TestEveryRegisteredPublicMethodHasExplicitPolicy(t *testing.T) {
	services := []*grpc.ServiceDesc{
		&adminv1.AccessAdmin_ServiceDesc, &adminv1.AdminAudit_ServiceDesc, &adminv1.AdminReliability_ServiceDesc,
		&adminv1.NodeAdmin_ServiceDesc, &adminv1.AllocationLifecycleAdmin_ServiceDesc, &adminv1.ServiceAdmin_ServiceDesc, &adminv1.StorageAdmin_ServiceDesc,
		&agentprofilev1.AgentProfileControl_ServiceDesc, &catalogv1.RuntimeCatalog_ServiceDesc, &environmentv1.EnvironmentControl_ServiceDesc,
		&functionv1.FunctionControl_ServiceDesc, &identityv1.IdentityControl_ServiceDesc, &namespacev1.NamespaceControl_ServiceDesc,
		&quotav1.QuotaControl_ServiceDesc, &rolloutv1.RolloutControl_ServiceDesc, &runv1.RunControl_ServiceDesc,
		&secretv1.SecretControl_ServiceDesc, &servicev1.ServiceControl_ServiceDesc, &tunnelv1.TunnelControl_ServiceDesc,
	}
	for _, service := range services {
		for _, method := range service.Methods {
			fullMethod := "/" + service.ServiceName + "/" + method.MethodName
			if _, ok := publicPolicy(fullMethod); !ok {
				t.Errorf("method %s has no authorization policy", fullMethod)
			}
		}
		for _, stream := range service.Streams {
			fullMethod := "/" + service.ServiceName + "/" + stream.StreamName
			if _, ok := publicPolicy(fullMethod); !ok {
				t.Errorf("method %s has no authorization policy", fullMethod)
			}
		}
	}
}

func TestNonPrefixReadMethodsAreReadOnly(t *testing.T) {
	methods := []string{
		"/axern.control.agentprofile.v1.AgentProfileControl/DoctorAgentProfile",
		"/axern.control.rollout.v1.RolloutControl/CompareRollouts",
		"/axern.control.rollout.v1.RolloutControl/DiagnoseRollout",
		"/axern.control.rollout.v1.RolloutControl/PrepareArtifactDownload",
		"/axern.control.tunnel.v1.TunnelControl/InspectTunnelSession",
	}
	for _, method := range methods {
		policy, ok := publicPolicy(method)
		if !ok || policy.action != accesskernel.ActionResourceRead {
			t.Errorf("method %s policy=(%q,%v), want resource read", method, policy.action, ok)
		}
	}
}

func TestUnknownMethodOnKnownServiceFailsClosed(t *testing.T) {
	if _, ok := publicPolicy("/axern.control.service.v1.ServiceControl/FutureMutation"); ok {
		t.Fatal("unknown method received an authorization policy")
	}
}

func TestUnknownControlServiceFailsClosed(t *testing.T) {
	i := &Interceptor{}
	called := false
	_, err := i.Unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/axern.control.future.v1.FutureControl/DoThing"}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if status.Code(err) != codes.PermissionDenied || called {
		t.Fatalf("unknown control method called=%v code=%v", called, status.Code(err))
	}
}

func TestGatewayControlRequiresGatewayPeer(t *testing.T) {
	i := &Interceptor{gatewayPeer: func(context.Context) bool { return false }}
	called := false
	_, err := i.Unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/axern.control.gateway.v1.GatewayControl/ResolveServiceRoute"}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated || called {
		t.Fatalf("non-gateway peer called=%v code=%v", called, status.Code(err))
	}
}

func TestNodeControlRequiresNodeWorkloadIdentity(t *testing.T) {
	i := &Interceptor{}
	method := "/axern.control.node.v1.NodeControl/RegisterNode"
	called := false
	handler := func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	}
	_, err := i.Unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
	if status.Code(err) != codes.Unauthenticated || called {
		t.Fatalf("unverified node called=%v code=%v", called, status.Code(err))
	}
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tlsState("axern-node")}})
	_, err = i.Unary(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
	if err != nil || !called {
		t.Fatalf("verified node called=%v err=%v", called, err)
	}
}

func TestTunnelRelayControlRequiresTunneldWorkloadIdentity(t *testing.T) {
	i := &Interceptor{}
	method := "/axern.private.control.tunnel.v1.TunnelRelayControl/ValidateTunnelPeer"
	called := false
	handler := func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	}
	wrongCtx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tlsState("gatewayd")}})
	_, err := i.Unary(wrongCtx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
	if status.Code(err) != codes.Unauthenticated || called {
		t.Fatalf("wrong workload called=%v code=%v", called, status.Code(err))
	}
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tlsState("tunneld")}})
	_, err = i.Unary(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
	if err != nil || !called {
		t.Fatalf("verified tunneld called=%v err=%v", called, err)
	}
}

func tlsState(commonName string) tls.ConnectionState {
	certificate := &x509.Certificate{Subject: pkix.Name{CommonName: commonName}}
	return tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{certificate}}}
}
