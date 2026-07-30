package adminv1

import (
	"context"
	"errors"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) CreatePrincipal(ctx context.Context, req *adminv1.CreatePrincipalRequest) (*adminv1.CreatePrincipalResponse, error) {
	p, err := s.deps.Access.CreatePrincipal(ctx, req.GetName(), req.GetDisplayName(), principalKind(req.GetKind()))
	if err != nil {
		return nil, accessError(err)
	}
	return &adminv1.CreatePrincipalResponse{Principal: principalProto(p)}, nil
}
func (s *Server) ListPrincipals(ctx context.Context, _ *adminv1.ListPrincipalsRequest) (*adminv1.ListPrincipalsResponse, error) {
	items, err := s.deps.Access.ListPrincipals(ctx)
	if err != nil {
		return nil, accessError(err)
	}
	out := make([]*adminv1.Principal, 0, len(items))
	for _, item := range items {
		out = append(out, principalProto(item))
	}
	return &adminv1.ListPrincipalsResponse{Principals: out}, nil
}
func (s *Server) DisablePrincipal(ctx context.Context, req *adminv1.DisablePrincipalRequest) (*adminv1.DisablePrincipalResponse, error) {
	p, err := s.deps.Access.DisablePrincipal(ctx, req.GetPrincipalID())
	if err != nil {
		return nil, accessError(err)
	}
	return &adminv1.DisablePrincipalResponse{Principal: principalProto(p)}, nil
}
func (s *Server) AddPrincipalCredential(ctx context.Context, req *adminv1.AddPrincipalCredentialRequest) (*adminv1.AddPrincipalCredentialResponse, error) {
	c, err := s.deps.Access.AddCredential(ctx, req.GetPrincipalID(), req.GetLabel(), req.GetCertificateDer())
	if err != nil {
		return nil, accessError(err)
	}
	return &adminv1.AddPrincipalCredentialResponse{Credential: credentialProto(c)}, nil
}
func (s *Server) ListPrincipalCredentials(ctx context.Context, req *adminv1.ListPrincipalCredentialsRequest) (*adminv1.ListPrincipalCredentialsResponse, error) {
	items, err := s.deps.Access.ListCredentials(ctx, req.GetPrincipalID())
	if err != nil {
		return nil, accessError(err)
	}
	out := make([]*adminv1.PrincipalCredential, 0, len(items))
	for _, item := range items {
		out = append(out, credentialProto(item))
	}
	return &adminv1.ListPrincipalCredentialsResponse{Credentials: out}, nil
}
func (s *Server) RevokePrincipalCredential(ctx context.Context, req *adminv1.RevokePrincipalCredentialRequest) (*adminv1.RevokePrincipalCredentialResponse, error) {
	c, err := s.deps.Access.RevokeCredential(ctx, req.GetCredentialID())
	if err != nil {
		return nil, accessError(err)
	}
	return &adminv1.RevokePrincipalCredentialResponse{Credential: credentialProto(c)}, nil
}
func (s *Server) GrantRoleBinding(ctx context.Context, req *adminv1.GrantRoleBindingRequest) (*adminv1.GrantRoleBindingResponse, error) {
	b, err := s.deps.Access.GrantBinding(ctx, req.GetPrincipalID(), scopeType(req.GetScopeType()), req.GetNamespace(), role(req.GetRole()))
	if err != nil {
		return nil, accessError(err)
	}
	return &adminv1.GrantRoleBindingResponse{Binding: bindingProto(b)}, nil
}
func (s *Server) ListRoleBindings(ctx context.Context, req *adminv1.ListRoleBindingsRequest) (*adminv1.ListRoleBindingsResponse, error) {
	items, err := s.deps.Access.ListBindings(ctx, req.GetPrincipalID(), req.GetNamespace(), req.GetIncludeRevoked())
	if err != nil {
		return nil, accessError(err)
	}
	out := make([]*adminv1.RoleBinding, 0, len(items))
	for _, item := range items {
		out = append(out, bindingProto(item))
	}
	return &adminv1.ListRoleBindingsResponse{Bindings: out}, nil
}
func (s *Server) RevokeRoleBinding(ctx context.Context, req *adminv1.RevokeRoleBindingRequest) (*adminv1.RevokeRoleBindingResponse, error) {
	b, err := s.deps.Access.RevokeBinding(ctx, req.GetBindingID())
	if err != nil {
		return nil, accessError(err)
	}
	return &adminv1.RevokeRoleBindingResponse{Binding: bindingProto(b)}, nil
}

func principalKind(value adminv1.PrincipalKind) accesskernel.PrincipalKind {
	switch value {
	case adminv1.PrincipalKind_PRINCIPAL_KIND_HUMAN:
		return accesskernel.PrincipalKindHuman
	case adminv1.PrincipalKind_PRINCIPAL_KIND_SERVICE:
		return accesskernel.PrincipalKindService
	default:
		return ""
	}
}
func scopeType(value adminv1.AccessScopeType) accesskernel.ScopeType {
	if value == adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_NAMESPACE {
		return accesskernel.ScopeNamespace
	}
	if value == adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_PLATFORM {
		return accesskernel.ScopePlatform
	}
	return ""
}
func role(value adminv1.AccessRole) accesskernel.Role {
	switch value {
	case adminv1.AccessRole_ACCESS_ROLE_PLATFORM_ADMIN:
		return accesskernel.RolePlatformAdmin
	case adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_ADMIN:
		return accesskernel.RoleNamespaceAdmin
	case adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_EDITOR:
		return accesskernel.RoleNamespaceEditor
	case adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_VIEWER:
		return accesskernel.RoleNamespaceViewer
	case adminv1.AccessRole_ACCESS_ROLE_ROLLOUT_EXECUTOR:
		return accesskernel.RoleRolloutExecutor
	default:
		return ""
	}
}
func principalProto(p accesskernel.Principal) *adminv1.Principal {
	return &adminv1.Principal{PrincipalID: p.ID, Name: p.Name, DisplayName: p.DisplayName, Kind: map[accesskernel.PrincipalKind]adminv1.PrincipalKind{accesskernel.PrincipalKindHuman: adminv1.PrincipalKind_PRINCIPAL_KIND_HUMAN, accesskernel.PrincipalKindService: adminv1.PrincipalKind_PRINCIPAL_KIND_SERVICE}[p.Kind], Status: map[accesskernel.PrincipalStatus]adminv1.PrincipalStatus{accesskernel.PrincipalStatusActive: adminv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE, accesskernel.PrincipalStatusDisabled: adminv1.PrincipalStatus_PRINCIPAL_STATUS_DISABLED}[p.Status], Version: p.Version, CreatedAt: timestamppb.New(p.CreatedAt), UpdatedAt: timestamppb.New(p.UpdatedAt)}
}
func credentialProto(c accesskernel.Credential) *adminv1.PrincipalCredential {
	out := &adminv1.PrincipalCredential{CredentialID: c.ID, PrincipalID: c.PrincipalID, Fingerprint: accesskernel.FormatFingerprint(c.Fingerprint), CertificateNotAfter: timestamppb.New(c.CertificateNotAfter), Label: c.Label, CreatedAt: timestamppb.New(c.CreatedAt)}
	if c.RevokedAt != nil {
		out.RevokedAt = timestamppb.New(*c.RevokedAt)
	}
	return out
}
func bindingProto(b accesskernel.Binding) *adminv1.RoleBinding {
	out := &adminv1.RoleBinding{BindingID: b.ID, PrincipalID: b.PrincipalID, ScopeType: map[accesskernel.ScopeType]adminv1.AccessScopeType{accesskernel.ScopePlatform: adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_PLATFORM, accesskernel.ScopeNamespace: adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_NAMESPACE}[b.Scope], Namespace: b.Namespace, Role: map[accesskernel.Role]adminv1.AccessRole{accesskernel.RolePlatformAdmin: adminv1.AccessRole_ACCESS_ROLE_PLATFORM_ADMIN, accesskernel.RoleNamespaceAdmin: adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_ADMIN, accesskernel.RoleNamespaceEditor: adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_EDITOR, accesskernel.RoleNamespaceViewer: adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_VIEWER, accesskernel.RoleRolloutExecutor: adminv1.AccessRole_ACCESS_ROLE_ROLLOUT_EXECUTOR}[b.Role], CreatedByPrincipalID: b.CreatedByPrincipalID, CreatedAt: timestamppb.New(b.CreatedAt), RevokedByPrincipalID: b.RevokedByPrincipalID}
	if b.RevokedAt != nil {
		out.RevokedAt = timestamppb.New(*b.RevokedAt)
	}
	return out
}
func accessError(err error) error {
	switch {
	case errors.Is(err, accesskernel.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "authenticated principal is required")
	case errors.Is(err, accesskernel.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, "permission denied")
	case errors.Is(err, accesskernel.ErrNotFound):
		return status.Error(codes.NotFound, "access resource not found")
	case errors.Is(err, accesskernel.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, "access resource already exists")
	case errors.Is(err, accesskernel.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, accesskernel.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, "access operation failed")
	}
}
