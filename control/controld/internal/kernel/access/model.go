package access

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound           = errors.New("access resource not found")
	ErrAlreadyExists      = errors.New("access resource already exists")
	ErrUnauthenticated    = errors.New("credential is not active")
	ErrPermissionDenied   = errors.New("permission denied")
	ErrInvalidArgument    = errors.New("invalid access argument")
	ErrFailedPrecondition = errors.New("access precondition failed")
)

type PrincipalKind string

const (
	PrincipalKindHuman   PrincipalKind = "human"
	PrincipalKindService PrincipalKind = "service"
)

type PrincipalStatus string

const (
	PrincipalStatusActive   PrincipalStatus = "active"
	PrincipalStatusDisabled PrincipalStatus = "disabled"
)

type ScopeType string

const (
	ScopePlatform  ScopeType = "platform"
	ScopeNamespace ScopeType = "namespace"
)

type Role string

const (
	RolePlatformAdmin   Role = "platform_admin"
	RoleNamespaceAdmin  Role = "namespace_admin"
	RoleNamespaceEditor Role = "namespace_editor"
	RoleNamespaceViewer Role = "namespace_viewer"
	RoleRolloutExecutor Role = "rollout_executor"
)

type Action string

const (
	ActionIdentityRead       Action = "identity.read"
	ActionCatalogRead        Action = "catalog.read"
	ActionNamespaceRead      Action = "namespace.read"
	ActionNamespaceManage    Action = "namespace.manage"
	ActionQuotaRead          Action = "quota.read"
	ActionQuotaManage        Action = "quota.manage"
	ActionResourceRead       Action = "resource.read"
	ActionResourceWrite      Action = "resource.write"
	ActionSandboxExecute     Action = "sandbox.execute"
	ActionNamespaceAccess    Action = "namespace.access.manage"
	ActionPlatformAccess     Action = "platform.access.manage"
	ActionPlatformAdmin      Action = "platform.admin"
	ActionRolloutWorkExecute Action = "rollout.work.execute"
)

type Principal struct {
	ID          string
	Name        string
	DisplayName string
	Kind        PrincipalKind
	Status      PrincipalStatus
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Credential struct {
	ID                  string
	PrincipalID         string
	Fingerprint         [sha256.Size]byte
	CertificateNotAfter time.Time
	Label               string
	CreatedAt           time.Time
	RevokedAt           *time.Time
}

type Binding struct {
	ID                   string
	PrincipalID          string
	Scope                ScopeType
	Namespace            string
	Role                 Role
	CreatedByPrincipalID string
	CreatedAt            time.Time
	RevokedByPrincipalID string
	RevokedAt            *time.Time
}

type Actor struct {
	Principal  Principal
	Credential Credential
	Bindings   []Binding
}

func (a Actor) PrincipalID() string { return a.Principal.ID }

type actorContextKey struct{}

func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorContextKey{}).(Actor)
	return actor, ok
}

var principalNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func ValidatePrincipalName(name string) error {
	if !principalNamePattern.MatchString(name) {
		return fmt.Errorf("%w: principal name must match %s", ErrInvalidArgument, principalNamePattern.String())
	}
	return nil
}

func ParseCertificateDER(der []byte) ([sha256.Size]byte, time.Time, error) {
	if len(der) == 0 {
		return [sha256.Size]byte{}, time.Time{}, fmt.Errorf("%w: certificate DER is required", ErrInvalidArgument)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return [sha256.Size]byte{}, time.Time{}, fmt.Errorf("%w: parse certificate DER: %v", ErrInvalidArgument, err)
	}
	return sha256.Sum256(certificate.Raw), certificate.NotAfter.UTC(), nil
}

func ParseFingerprint(value string) ([sha256.Size]byte, error) {
	var fingerprint [sha256.Size]byte
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return fingerprint, errors.New("certificate fingerprint must be 64 lowercase hexadecimal characters")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return fingerprint, errors.New("certificate fingerprint must be 64 lowercase hexadecimal characters")
	}
	copy(fingerprint[:], decoded)
	return fingerprint, nil
}

func FormatFingerprint(fingerprint [sha256.Size]byte) string {
	return hex.EncodeToString(fingerprint[:])
}

func IsPublicRole(role Role) bool {
	switch role {
	case RolePlatformAdmin, RoleNamespaceAdmin, RoleNamespaceEditor, RoleNamespaceViewer:
		return true
	default:
		return false
	}
}
