package secretkernel

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
)

type ResolvedSecret struct {
	ID   string
	Type secretv1.SecretType
	Data map[string]string
}

func NormalizeMasterKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("secrets master key is required")
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("secrets master key must be 32 raw bytes or base64-encoded 32 bytes")
}

type MetadataReader interface {
	Get(ctx context.Context, id string) (*secretv1.Secret, bool, error)
	List(ctx context.Context, filter *secretv1.SecretListFilter) ([]*secretv1.Secret, error)
}

type Mutator interface {
	Create(ctx context.Context, params CreateParams, now time.Time) (*secretv1.Secret, error)
	Delete(ctx context.Context, id string) (*secretv1.Secret, bool, error)
}

type ValueResolver interface {
	Resolve(ctx context.Context, id string) (*ResolvedSecret, bool, error)
}

// ProfileCredentialResolver is intentionally separate from ValueResolver.
// Hidden Profile-owned credentials must never become addressable through a
// generic workload Secret reference.
type ProfileCredentialResolver interface {
	ResolveProfileCredential(ctx context.Context, id string) (*ResolvedSecret, bool, error)
}

type DockerConfigResolver interface {
	ResolveDockerConfigJSON(ctx context.Context, id string) (string, bool, error)
}

type Control interface {
	MetadataReader
	Mutator
	ValueResolver
	DockerConfigResolver
}

type CreateParams struct {
	Namespace  string
	SecretType secretv1.SecretType
	StringData map[string]string
	Labels     map[string]string
}
