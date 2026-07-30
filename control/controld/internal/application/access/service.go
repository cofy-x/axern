package access

import (
	"context"
	"fmt"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
)

type Store interface {
	ResolveActor(context.Context, [32]byte, time.Time) (accesskernel.Actor, error)
	HasActivePlatformAdmin(context.Context, time.Time) (bool, error)
	CreatePrincipal(context.Context, string, string, string, accesskernel.PrincipalKind, time.Time) (accesskernel.Principal, error)
	ListPrincipals(context.Context) ([]accesskernel.Principal, error)
	DisablePrincipal(context.Context, string, string, time.Time) (accesskernel.Principal, error)
	AddCredential(context.Context, string, string, string, [32]byte, time.Time, time.Time) (accesskernel.Credential, error)
	ListCredentials(context.Context, string) ([]accesskernel.Credential, error)
	RevokeCredential(context.Context, string, string, time.Time) (accesskernel.Credential, error)
	GrantBinding(context.Context, string, string, accesskernel.ScopeType, string, accesskernel.Role, time.Time) (accesskernel.Binding, error)
	GetBinding(context.Context, string) (accesskernel.Binding, error)
	ListBindings(context.Context, string, string, bool) ([]accesskernel.Binding, error)
	RevokeBinding(context.Context, string, string, time.Time) (accesskernel.Binding, error)
	ResolveResourceNamespace(context.Context, string, string) (string, error)
	ValidateRolloutExecutionLease(context.Context, string, string, time.Time) error
}

type Service struct {
	store Store
	now   func() time.Time
}

func New(store Store, now func() time.Time) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, now: now}
}

func (s *Service) ResolveActor(ctx context.Context, fingerprint [32]byte) (accesskernel.Actor, error) {
	return s.store.ResolveActor(ctx, fingerprint, s.now())
}

func (s *Service) HasActivePlatformAdmin(ctx context.Context) (bool, error) {
	return s.store.HasActivePlatformAdmin(ctx, s.now())
}

func (s *Service) ResolveResourceNamespace(ctx context.Context, resourceType, resourceID string) (string, error) {
	return s.store.ResolveResourceNamespace(ctx, resourceType, resourceID)
}

func (s *Service) ValidateRolloutExecutionLease(ctx context.Context, token, namespace string) error {
	return s.store.ValidateRolloutExecutionLease(ctx, token, namespace, s.now())
}

func (s *Service) AuthorizeFingerprintResource(ctx context.Context, fingerprintValue, rolloutExecutionLease string, action accesskernel.Action, resourceType, resourceID string) error {
	fingerprint, err := accesskernel.ParseFingerprint(fingerprintValue)
	if err != nil {
		return accesskernel.ErrUnauthenticated
	}
	actor, err := s.ResolveActor(ctx, fingerprint)
	if err != nil {
		return err
	}
	namespace, err := s.ResolveResourceNamespace(ctx, resourceType, resourceID)
	if err != nil {
		return err
	}
	if accesskernel.Authorize(actor, action, namespace) {
		return nil
	}
	if !accesskernel.HasRole(actor, accesskernel.RoleRolloutExecutor) || !accesskernel.IsRolloutDelegatableAction(action) {
		return accesskernel.ErrPermissionDenied
	}
	return s.ValidateRolloutExecutionLease(ctx, rolloutExecutionLease, namespace)
}

func actor(ctx context.Context) (accesskernel.Actor, error) {
	value, ok := accesskernel.ActorFromContext(ctx)
	if !ok {
		return accesskernel.Actor{}, accesskernel.ErrUnauthenticated
	}
	return value, nil
}

func require(ctx context.Context, action accesskernel.Action, namespace string) (accesskernel.Actor, error) {
	value, err := actor(ctx)
	if err != nil {
		return accesskernel.Actor{}, err
	}
	if !accesskernel.Authorize(value, action, namespace) {
		return accesskernel.Actor{}, accesskernel.ErrPermissionDenied
	}
	return value, nil
}

func (s *Service) CreatePrincipal(ctx context.Context, name, displayName string, kind accesskernel.PrincipalKind) (accesskernel.Principal, error) {
	a, err := require(ctx, accesskernel.ActionPlatformAccess, "")
	if err != nil {
		return accesskernel.Principal{}, err
	}
	return s.store.CreatePrincipal(ctx, a.Principal.ID, name, displayName, kind, s.now())
}
func (s *Service) ListPrincipals(ctx context.Context) ([]accesskernel.Principal, error) {
	if _, err := require(ctx, accesskernel.ActionPlatformAccess, ""); err != nil {
		return nil, err
	}
	return s.store.ListPrincipals(ctx)
}
func (s *Service) DisablePrincipal(ctx context.Context, id string) (accesskernel.Principal, error) {
	a, err := require(ctx, accesskernel.ActionPlatformAccess, "")
	if err != nil {
		return accesskernel.Principal{}, err
	}
	if a.Principal.ID == id {
		return accesskernel.Principal{}, fmt.Errorf("%w: a principal cannot disable itself", accesskernel.ErrFailedPrecondition)
	}
	return s.store.DisablePrincipal(ctx, a.Principal.ID, id, s.now())
}
func (s *Service) AddCredential(ctx context.Context, principalID, label string, der []byte) (accesskernel.Credential, error) {
	a, err := require(ctx, accesskernel.ActionPlatformAccess, "")
	if err != nil {
		return accesskernel.Credential{}, err
	}
	fingerprint, notAfter, err := accesskernel.ParseCertificateDER(der)
	if err != nil {
		return accesskernel.Credential{}, err
	}
	return s.store.AddCredential(ctx, a.Principal.ID, principalID, label, fingerprint, notAfter, s.now())
}
func (s *Service) ListCredentials(ctx context.Context, principalID string) ([]accesskernel.Credential, error) {
	if _, err := require(ctx, accesskernel.ActionPlatformAccess, ""); err != nil {
		return nil, err
	}
	return s.store.ListCredentials(ctx, principalID)
}
func (s *Service) RevokeCredential(ctx context.Context, id string) (accesskernel.Credential, error) {
	a, err := require(ctx, accesskernel.ActionPlatformAccess, "")
	if err != nil {
		return accesskernel.Credential{}, err
	}
	if a.Credential.ID == id {
		return accesskernel.Credential{}, fmt.Errorf("%w: the active credential cannot revoke itself", accesskernel.ErrFailedPrecondition)
	}
	return s.store.RevokeCredential(ctx, a.Principal.ID, id, s.now())
}
func (s *Service) GrantBinding(ctx context.Context, principalID string, scope accesskernel.ScopeType, namespace string, role accesskernel.Role) (accesskernel.Binding, error) {
	a, err := actor(ctx)
	if err != nil {
		return accesskernel.Binding{}, err
	}
	if !accesskernel.CanGrant(a, role, namespace) {
		return accesskernel.Binding{}, accesskernel.ErrPermissionDenied
	}
	return s.store.GrantBinding(ctx, a.Principal.ID, principalID, scope, namespace, role, s.now())
}
func (s *Service) ListBindings(ctx context.Context, principalID, namespace string, includeRevoked bool) ([]accesskernel.Binding, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if namespace == "" {
		if !accesskernel.Authorize(a, accesskernel.ActionPlatformAccess, "") {
			return nil, accesskernel.ErrPermissionDenied
		}
	} else if !accesskernel.Authorize(a, accesskernel.ActionNamespaceAccess, namespace) {
		return nil, accesskernel.ErrPermissionDenied
	}
	return s.store.ListBindings(ctx, principalID, namespace, includeRevoked)
}
func (s *Service) RevokeBinding(ctx context.Context, id string) (accesskernel.Binding, error) {
	a, err := actor(ctx)
	if err != nil {
		return accesskernel.Binding{}, err
	}
	binding, err := s.store.GetBinding(ctx, id)
	if err != nil {
		return accesskernel.Binding{}, err
	}
	if !accesskernel.CanGrant(a, binding.Role, binding.Namespace) {
		return accesskernel.Binding{}, accesskernel.ErrNotFound
	}
	return s.store.RevokeBinding(ctx, a.Principal.ID, id, s.now())
}
