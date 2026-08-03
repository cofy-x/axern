package authz

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"
	"sync"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const ClientCertificateFingerprintMetadata = "x-axern-internal-client-cert-sha256"
const RolloutExecutionLeaseMetadata = "x-axern-rollout-work-lease"

type AccessResolver interface {
	ResolveActor(context.Context, [32]byte) (accesskernel.Actor, error)
	ResolveResourceNamespace(context.Context, string, string) (string, error)
	ValidateRolloutExecutionLease(context.Context, string, string) error
}

type Interceptor struct {
	access          AccessResolver
	gatewayPeer     func(context.Context) bool
	recheckInterval time.Duration
}

func New(access AccessResolver) *Interceptor {
	return &Interceptor{access: access, gatewayPeer: isGatewayPeer, recheckInterval: 15 * time.Second}
}

func (i *Interceptor) Unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if isTunnelRelayControlMethod(info.FullMethod) {
		if !isWorkloadPeer(ctx, "tunneld") {
			return nil, status.Error(codes.Unauthenticated, "tunneld mTLS identity is required")
		}
		return handler(ctx, req)
	}
	if isNodeControlMethod(info.FullMethod) {
		if !isWorkloadPeer(ctx, "axern-node") {
			return nil, status.Error(codes.Unauthenticated, "axern-node mTLS identity is required")
		}
		return handler(ctx, req)
	}
	if isGatewayControlMethod(info.FullMethod) {
		if i.gatewayPeer == nil || !i.gatewayPeer(ctx) {
			return nil, status.Error(codes.Unauthenticated, "gatewayd mTLS identity is required")
		}
		return handler(ctx, req)
	}
	if isRolloutWorkerMethod(info.FullMethod) {
		actorCtx, err := i.authenticateRolloutWorker(ctx)
		if err != nil {
			recordDecision(ctx, accesskernel.ActionRolloutWorkExecute, "unauthenticated")
			return nil, err
		}
		recordDecision(actorCtx, accesskernel.ActionRolloutWorkExecute, "allow")
		return handler(actorCtx, req)
	}
	policy, protected := publicPolicy(info.FullMethod)
	if !protected {
		if isUnclassifiedControlMethod(info.FullMethod) {
			return nil, status.Error(codes.PermissionDenied, "control method has no authorization policy")
		}
		return handler(ctx, req)
	}
	actorCtx, actor, err := i.authenticate(ctx)
	if err != nil {
		recordDecision(ctx, policy.action, "unauthenticated")
		return nil, err
	}
	namespace, err := i.namespace(actorCtx, policy, req)
	if err != nil {
		recordDecision(actorCtx, policy.action, "error")
		return nil, err
	}
	if err := i.authorize(actorCtx, actor, policy.action, namespace); err != nil {
		if policy.resourceType != "" && errors.Is(err, accesskernel.ErrPermissionDenied) && namespace != "" && !hasExplicitNamespace(req) &&
			!accesskernel.Authorize(actor, accesskernel.ActionResourceRead, namespace) && !accesskernel.HasRole(actor, accesskernel.RoleRolloutExecutor) {
			recordDecision(actorCtx, policy.action, "deny")
			return nil, status.Error(codes.NotFound, "resource not found")
		}
		recordDecision(actorCtx, policy.action, "deny")
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
	recordDecision(actorCtx, policy.action, "allow")
	return handler(actorCtx, req)
}

func recordDecision(ctx context.Context, action accesskernel.Action, result string) {
	sdkobs.Int64Counter(ctrlobs.MetricAuthorizationDecisionTotal.Name, ctrlobs.MetricAuthorizationDecisionTotal.Description).Add(ctx, 1,
		attribute.String("axern.authorization.action", string(action)),
		attribute.String(sdkobs.AttrResult, result),
	)
}

func (i *Interceptor) Stream(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if isTunnelRelayControlMethod(info.FullMethod) {
		if !isWorkloadPeer(stream.Context(), "tunneld") {
			return status.Error(codes.Unauthenticated, "tunneld mTLS identity is required")
		}
		return handler(srv, stream)
	}
	if isNodeControlMethod(info.FullMethod) {
		if !isWorkloadPeer(stream.Context(), "axern-node") {
			return status.Error(codes.Unauthenticated, "axern-node mTLS identity is required")
		}
		return handler(srv, stream)
	}
	if isGatewayControlMethod(info.FullMethod) {
		if i.gatewayPeer == nil || !i.gatewayPeer(stream.Context()) {
			return status.Error(codes.Unauthenticated, "gatewayd mTLS identity is required")
		}
		return handler(srv, stream)
	}
	if isRolloutWorkerMethod(info.FullMethod) {
		actorCtx, err := i.authenticateRolloutWorker(stream.Context())
		if err != nil {
			recordDecision(stream.Context(), accesskernel.ActionRolloutWorkExecute, "unauthenticated")
			return err
		}
		recordDecision(actorCtx, accesskernel.ActionRolloutWorkExecute, "allow")
		return handler(srv, &contextServerStream{ServerStream: stream, ctx: actorCtx})
	}
	policy, protected := publicPolicy(info.FullMethod)
	if !protected {
		if isUnclassifiedControlMethod(info.FullMethod) {
			return status.Error(codes.PermissionDenied, "control method has no authorization policy")
		}
		return handler(srv, stream)
	}
	actorCtx, actor, err := i.authenticate(stream.Context())
	if err != nil {
		recordDecision(stream.Context(), policy.action, "unauthenticated")
		return err
	}
	if policy.action == accesskernel.ActionPlatformAdmin && i.authorize(actorCtx, actor, policy.action, "") != nil {
		recordDecision(actorCtx, policy.action, "deny")
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	streamCtx, cancel := context.WithCancel(actorCtx)
	defer cancel()
	return handler(srv, &actorServerStream{ServerStream: stream, ctx: streamCtx, interceptor: i, policy: policy, actor: actor, cancel: cancel})
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context { return s.ctx }

func isRolloutWorkerMethod(method string) bool {
	return strings.HasPrefix(method, "/axern.private.rollout.worker.v1.RolloutWorkerControl/")
}

func isTunnelRelayControlMethod(method string) bool {
	return strings.HasPrefix(method, "/axern.private.control.tunnel.v1.TunnelRelayControl/")
}

func isGatewayControlMethod(method string) bool {
	return strings.HasPrefix(method, "/axern.control.gateway.v1.GatewayControl/")
}

func isNodeControlMethod(method string) bool {
	return strings.HasPrefix(method, "/axern.control.node.v1.NodeControl/")
}

func isUnclassifiedControlMethod(method string) bool {
	if !strings.HasPrefix(method, "/axern.control.") {
		return false
	}
	for _, internalService := range []string{
		"/axern.control.gateway.v1.GatewayControl/",
		"/axern.control.node.v1.NodeControl/",
	} {
		if strings.HasPrefix(method, internalService) {
			return false
		}
	}
	return true
}

func (i *Interceptor) authenticateRolloutWorker(ctx context.Context) (context.Context, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ctx, status.Error(codes.Unauthenticated, "rollout worker mTLS identity is required")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.VerifiedChains[0]) == 0 {
		return ctx, status.Error(codes.Unauthenticated, "rollout worker mTLS identity is required")
	}
	fingerprint, _, err := accesskernel.ParseCertificateDER(info.State.VerifiedChains[0][0].Raw)
	if err != nil {
		return ctx, status.Error(codes.Unauthenticated, "rollout worker mTLS identity is invalid")
	}
	actor, err := i.access.ResolveActor(ctx, fingerprint)
	if err != nil || !accesskernel.HasRole(actor, accesskernel.RoleRolloutExecutor) {
		return ctx, status.Error(codes.Unauthenticated, "rollout worker credential is not active")
	}
	return accesskernel.WithActor(ctx, actor), nil
}

type actorServerStream struct {
	grpc.ServerStream
	ctx         context.Context
	interceptor *Interceptor
	policy      methodPolicy
	actor       accesskernel.Actor
	cancel      context.CancelFunc
	authorized  bool
	recheckOnce sync.Once
}

func (s *actorServerStream) Context() context.Context { return s.ctx }
func (s *actorServerStream) RecvMsg(message any) error {
	if err := s.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	if s.authorized {
		return nil
	}
	namespace, err := s.interceptor.namespace(s.ctx, s.policy, message)
	if err != nil {
		return err
	}
	if s.interceptor.authorize(s.ctx, s.actor, s.policy.action, namespace) != nil {
		recordDecision(s.ctx, s.policy.action, "deny")
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	recordDecision(s.ctx, s.policy.action, "allow")
	s.authorized = true
	s.recheckOnce.Do(func() { go s.recheck(namespace) })
	return nil
}

func (s *actorServerStream) recheck(namespace string) {
	interval := s.interceptor.recheckInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			actor, err := s.interceptor.access.ResolveActor(s.ctx, s.actor.Credential.Fingerprint)
			if err != nil || s.interceptor.authorize(accesskernel.WithActor(s.ctx, actor), actor, s.policy.action, namespace) != nil {
				recordDecision(s.ctx, s.policy.action, "revoked")
				s.cancel()
				return
			}
		}
	}
}

func (i *Interceptor) authorize(ctx context.Context, actor accesskernel.Actor, action accesskernel.Action, namespace string) error {
	if accesskernel.Authorize(actor, action, namespace) {
		return nil
	}
	if !accesskernel.HasRole(actor, accesskernel.RoleRolloutExecutor) || !accesskernel.IsRolloutDelegatableAction(action) {
		return accesskernel.ErrPermissionDenied
	}
	values := metadata.ValueFromIncomingContext(ctx, RolloutExecutionLeaseMetadata)
	if len(values) != 1 {
		return accesskernel.ErrPermissionDenied
	}
	if err := i.access.ValidateRolloutExecutionLease(ctx, values[0], namespace); err != nil {
		return accesskernel.ErrPermissionDenied
	}
	return nil
}

func hasExplicitNamespace(req any) bool {
	message, ok := req.(proto.Message)
	return ok && findStringField(message.ProtoReflect(), "namespace") != ""
}

func (i *Interceptor) authenticate(ctx context.Context) (context.Context, accesskernel.Actor, error) {
	if i.gatewayPeer == nil || !i.gatewayPeer(ctx) {
		return ctx, accesskernel.Actor{}, status.Error(codes.Unauthenticated, "public control requests must enter through gatewayd")
	}
	values := metadata.ValueFromIncomingContext(ctx, ClientCertificateFingerprintMetadata)
	if len(values) != 1 {
		return ctx, accesskernel.Actor{}, status.Error(codes.Unauthenticated, "client certificate identity is required")
	}
	fingerprint, err := accesskernel.ParseFingerprint(values[0])
	if err != nil {
		return ctx, accesskernel.Actor{}, status.Error(codes.Unauthenticated, "client certificate identity is invalid")
	}
	actor, err := i.access.ResolveActor(ctx, fingerprint)
	if err != nil {
		if errors.Is(err, accesskernel.ErrUnauthenticated) {
			return ctx, accesskernel.Actor{}, status.Error(codes.Unauthenticated, "client credential is not active")
		}
		return ctx, accesskernel.Actor{}, err
	}
	return accesskernel.WithActor(ctx, actor), actor, nil
}

func isGatewayPeer(ctx context.Context) bool {
	return isWorkloadPeer(ctx, "gatewayd")
}

func isWorkloadPeer(ctx context.Context, identity string) bool {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return false
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.VerifiedChains[0]) == 0 {
		return false
	}
	return certificateIdentity(info.State.VerifiedChains[0][0]) == identity
}
func certificateIdentity(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	return strings.TrimSpace(cert.Subject.CommonName)
}

type methodPolicy struct {
	action       accesskernel.Action
	resourceType string
}

func publicPolicy(method string) (methodPolicy, bool) {
	service := strings.TrimPrefix(method, "/")
	service, methodName, _ := strings.Cut(service, "/")
	switch service {
	case "axern.control.identity.v1.IdentityControl":
		return exactPolicy(methodName, accesskernel.ActionIdentityRead, "", "WhoAmI")
	case "axern.control.catalog.v1.RuntimeCatalog":
		return exactPolicy(methodName, accesskernel.ActionCatalogRead, "", "ListRuntimeTemplates", "GetRuntimeTemplate", "ListAgentBundles", "GetAgentBundle")
	case "axern.control.admin.v1.AccessAdmin":
		return exactPolicy(methodName, accesskernel.ActionIdentityRead, "",
			"CreatePrincipal", "ListPrincipals", "DisablePrincipal", "AddPrincipalCredential", "ListPrincipalCredentials",
			"RevokePrincipalCredential", "GrantRoleBinding", "ListRoleBindings", "RevokeRoleBinding")
	case "axern.control.admin.v1.AdminAudit":
		return exactPolicy(methodName, accesskernel.ActionPlatformAdmin, "", "ListAdminAuditEvents")
	case "axern.control.admin.v1.AdminReliability":
		return exactPolicy(methodName, accesskernel.ActionPlatformAdmin, "", "CheckConsistency", "GetAdminReliabilityHealth")
	case "axern.control.admin.v1.NodeAdmin":
		return exactPolicy(methodName, accesskernel.ActionPlatformAdmin, "", "ListAdminNodes", "RetireAdminNode")
	case "axern.control.admin.v1.AllocationLifecycleAdmin":
		return exactPolicy(methodName, accesskernel.ActionPlatformAdmin, "", "ListAllocationLifecycleRetries", "ForceAllocationLifecycleRetry", "FailAllocationLifecycleRetry", "ClearAllocationLifecycleRetry")
	case "axern.control.admin.v1.ServiceAdmin":
		return exactPolicy(methodName, accesskernel.ActionPlatformAdmin, "", "PurgeService")
	case "axern.control.admin.v1.StorageAdmin":
		return exactPolicy(methodName, accesskernel.ActionPlatformAdmin, "", "ListStorageBindings", "RetryStorageBinding", "ListStorageReclaims")
	case "axern.control.namespace.v1.NamespaceControl":
		if policy, ok := exactPolicy(methodName, accesskernel.ActionNamespaceManage, "", "CreateNamespace", "DeleteNamespace"); ok {
			return policy, true
		}
		if policy, ok := exactPolicy(methodName, accesskernel.ActionIdentityRead, "", "ListNamespaces"); ok {
			return policy, true
		}
		return exactPolicy(methodName, accesskernel.ActionNamespaceRead, "", "GetNamespace")
	case "axern.control.quota.v1.QuotaControl":
		if policy, ok := exactPolicy(methodName, accesskernel.ActionQuotaManage, "", "SetNamespaceQuota", "UnsetNamespaceQuota"); ok {
			return policy, true
		}
		if policy, ok := exactPolicy(methodName, accesskernel.ActionIdentityRead, "", "ListNamespaceQuotas"); ok {
			return policy, true
		}
		return exactPolicy(methodName, accesskernel.ActionQuotaRead, "", "GetNamespaceQuota", "ListNamespaceQuotaEvents")
	case "axern.control.environment.v1.EnvironmentControl":
		return resourcePolicy(methodName, "environment", []string{"GetEnvironment", "ListEnvironments"}, []string{"CreateEnvironment", "DeleteEnvironment"})
	case "axern.control.run.v1.RunControl":
		return resourcePolicy(methodName, "run", []string{"GetRun", "WatchRun", "ListRuns"}, []string{"CreateRun", "CancelRun"})
	case "axern.control.secret.v1.SecretControl":
		return resourcePolicy(methodName, "secret", []string{"GetSecret", "ListSecrets"}, []string{"CreateSecret", "DeleteSecret"})
	case "axern.control.service.v1.ServiceControl":
		return resourcePolicy(methodName, "service",
			[]string{"GetService", "WatchService", "GetServiceReplica", "ListServices", "ListServiceReplicas", "ListServiceEvents"},
			[]string{"CreateService", "UpdateService", "DeleteService"})
	case "axern.control.function.v1.FunctionControl":
		if methodName == "GetFunctionInvocation" {
			return methodPolicy{action: accesskernel.ActionResourceRead, resourceType: "function_invocation"}, true
		}
		return resourcePolicy(methodName, "function",
			[]string{"GetFunction", "ListFunctions", "ListFunctionInvocations", "ListFunctionEvents"},
			[]string{"UploadFunctionBundle", "DeployFunction", "DeleteFunction", "InvokeFunction"})
	case "axern.control.tunnel.v1.TunnelControl":
		return resourcePolicy(methodName, "tunnel",
			[]string{"GetTunnelSession", "ListTunnelSessions", "ListTunnelSessionEvents", "InspectTunnelSession"},
			[]string{"CreateTunnelSession", "RevokeTunnelSession", "RenewTunnelSession"})
	case "axern.control.agentprofile.v1.AgentProfileControl":
		return resourcePolicy(methodName, "profile",
			[]string{"GetAgentProfile", "ListAgentProfiles", "DoctorAgentProfile"},
			[]string{"CreateAgentProfile", "UpdateAgentProfile", "RotateAgentProfileCredential", "DeleteAgentProfile"})
	case "axern.control.rollout.v1.RolloutControl":
		if methodName == "PrepareArtifactDownload" {
			return methodPolicy{action: accesskernel.ActionResourceRead, resourceType: "rollout_artifact"}, true
		}
		return resourcePolicy(methodName, "rollout",
			[]string{"GetRollout", "ListRollouts", "WatchRolloutEvents", "CompareRollouts", "DiagnoseRollout", "ListArtifacts"},
			[]string{"CreateRollout", "StartRollout", "CancelRollout", "RetryRollout", "DeleteRollout"})
	default:
		return methodPolicy{}, false
	}
}

func exactPolicy(methodName string, action accesskernel.Action, resourceType string, methods ...string) (methodPolicy, bool) {
	for _, method := range methods {
		if methodName == method {
			return methodPolicy{action: action, resourceType: resourceType}, true
		}
	}
	return methodPolicy{}, false
}

func resourcePolicy(methodName, resourceType string, reads, writes []string) (methodPolicy, bool) {
	if policy, ok := exactPolicy(methodName, accesskernel.ActionResourceRead, resourceType, reads...); ok {
		return policy, true
	}
	return exactPolicy(methodName, accesskernel.ActionResourceWrite, resourceType, writes...)
}

func (i *Interceptor) namespace(ctx context.Context, policy methodPolicy, req any) (string, error) {
	if policy.action == accesskernel.ActionIdentityRead || policy.action == accesskernel.ActionCatalogRead || policy.action == accesskernel.ActionPlatformAdmin || policy.action == accesskernel.ActionNamespaceManage {
		return "", nil
	}
	message, ok := req.(proto.Message)
	if !ok {
		return "", status.Error(codes.Internal, "authorization request is not protobuf")
	}
	if namespace := findStringField(message.ProtoReflect(), "namespace"); namespace != "" {
		return namespace, nil
	}
	if rolloutIDs := findRepeatedStringField(message.ProtoReflect(), "rollout_ids"); len(rolloutIDs) > 0 {
		var namespace string
		for _, rolloutID := range rolloutIDs {
			resolved, err := i.resolveResourceNamespace(ctx, "rollout", rolloutID)
			if err != nil {
				return "", err
			}
			if namespace != "" && namespace != resolved {
				return "", status.Error(codes.NotFound, "resource not found")
			}
			namespace = resolved
		}
		return namespace, nil
	}
	if policy.resourceType != "" {
		field := policy.resourceType + "_id"
		if policy.resourceType == "tunnel" {
			field = "session_id"
		}
		if id := findStringField(message.ProtoReflect(), protoreflect.Name(field)); id != "" {
			return i.resolveResourceNamespace(ctx, policy.resourceType, id)
		}
		if allocationID := findStringField(message.ProtoReflect(), "allocation_id"); allocationID != "" {
			return i.resolveResourceNamespace(ctx, "allocation", allocationID)
		}
		for _, candidate := range []struct {
			field        protoreflect.Name
			resourceType string
		}{
			{field: "invocation_id", resourceType: "function_invocation"},
			{field: "revision_id", resourceType: "function_revision"},
			{field: "artifact_id", resourceType: "rollout_artifact"},
		} {
			if id := findStringField(message.ProtoReflect(), candidate.field); id != "" {
				return i.resolveResourceNamespace(ctx, candidate.resourceType, id)
			}
		}
	}
	return "", nil
}

func (i *Interceptor) resolveResourceNamespace(ctx context.Context, resourceType, resourceID string) (string, error) {
	namespace, err := i.access.ResolveResourceNamespace(ctx, resourceType, resourceID)
	if errors.Is(err, accesskernel.ErrNotFound) {
		return "", status.Error(codes.NotFound, "resource not found")
	}
	return namespace, err
}

func findStringField(message protoreflect.Message, name protoreflect.Name) string {
	fields := message.Descriptor().Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		if field.Name() == name && field.Kind() == protoreflect.StringKind {
			if field.IsList() || field.IsMap() {
				continue
			}
			return message.Get(field).String()
		}
		for _, nested := range nestedMessages(message, field) {
			if value := findStringField(nested, name); value != "" {
				return value
			}
		}
	}
	return ""
}

func findRepeatedStringField(message protoreflect.Message, name protoreflect.Name) []string {
	fields := message.Descriptor().Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		if field.Name() == name && field.IsList() && field.Kind() == protoreflect.StringKind {
			list := message.Get(field).List()
			values := make([]string, 0, list.Len())
			for item := 0; item < list.Len(); item++ {
				if value := strings.TrimSpace(list.Get(item).String()); value != "" {
					values = append(values, value)
				}
			}
			return values
		}
		for _, nested := range nestedMessages(message, field) {
			if values := findRepeatedStringField(nested, name); len(values) > 0 {
				return values
			}
		}
	}
	return nil
}

func nestedMessages(message protoreflect.Message, field protoreflect.FieldDescriptor) []protoreflect.Message {
	if field.Kind() != protoreflect.MessageKind || !message.Has(field) {
		return nil
	}
	value := message.Get(field)
	if field.IsMap() {
		if field.MapValue().Kind() != protoreflect.MessageKind {
			return nil
		}
		messages := make([]protoreflect.Message, 0, value.Map().Len())
		value.Map().Range(func(_ protoreflect.MapKey, value protoreflect.Value) bool {
			messages = append(messages, value.Message())
			return true
		})
		return messages
	}
	if field.IsList() {
		list := value.List()
		messages := make([]protoreflect.Message, 0, list.Len())
		for index := 0; index < list.Len(); index++ {
			messages = append(messages, list.Get(index).Message())
		}
		return messages
	}
	return []protoreflect.Message{value.Message()}
}
