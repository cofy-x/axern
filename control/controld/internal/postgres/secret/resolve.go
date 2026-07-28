package pgsecret

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) Resolve(ctx context.Context, id string) (*secretkernel.ResolvedSecret, bool, error) {
	return s.resolveRecord(ctx, getRecordTx, id)
}

func (s *Store) ResolveProfileCredential(ctx context.Context, id string) (*secretkernel.ResolvedSecret, bool, error) {
	return s.resolveRecord(ctx, getProfileCredentialRecordTx, id)
}

func (s *Store) resolveRecord(ctx context.Context, read func(context.Context, rowQuery, string) (*secretv1.Secret, []byte, error), id string) (*secretkernel.ResolvedSecret, bool, error) {
	secret, ciphertext, err := read(ctx, s.db.Pool(), strings.TrimSpace(id))
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	plaintext, err := s.decrypt(ciphertext)
	if err != nil {
		return nil, false, err
	}
	data := map[string]string{}
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return nil, false, fmt.Errorf("unmarshal decrypted secret payload: %w", err)
	}
	return &secretkernel.ResolvedSecret{ID: secret.GetID(), Type: secret.GetType(), Data: data}, true, nil
}

func (s *Store) ResolveDockerConfigJSON(ctx context.Context, id string) (string, bool, error) {
	resolved, ok, err := s.Resolve(ctx, id)
	if err != nil || !ok {
		return "", ok, err
	}
	if resolved.Type != secretv1.SecretType_SECRET_TYPE_DOCKER_CONFIG_JSON {
		return "", false, grpcstatus.Errorf(codes.InvalidArgument, "secret %q is not a docker-config-json secret", id)
	}
	return resolved.Data[dockerConfigJSONKey], true, nil
}
