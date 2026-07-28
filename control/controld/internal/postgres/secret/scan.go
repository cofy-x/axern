package pgsecret

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type rowQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getRecordTx(ctx context.Context, q rowQuery, id string) (*secretv1.Secret, []byte, error) {
	var ciphertext []byte
	row := q.QueryRow(ctx, `
		SELECT secret_id, namespace, type, data_keys, labels, version, created_at, updated_at, encrypted_payload
		FROM secrets
		WHERE secret_id = $1 AND visibility = 'PUBLIC'
	`, strings.TrimSpace(id))
	secret, err := scanSecretMetadataWithCiphertext(row, &ciphertext)
	if err != nil {
		return nil, nil, err
	}
	return secret, ciphertext, nil
}

func getProfileCredentialRecordTx(ctx context.Context, q rowQuery, id string) (*secretv1.Secret, []byte, error) {
	var ciphertext []byte
	row := q.QueryRow(ctx, `
		SELECT secret_id, namespace, type, data_keys, labels, version, created_at, updated_at, encrypted_payload
		FROM secrets
		WHERE secret_id = $1 AND visibility = 'INTERNAL' AND owner_type = 'AGENT_PROFILE'
	`, strings.TrimSpace(id))
	secret, err := scanSecretMetadataWithCiphertext(row, &ciphertext)
	if err != nil {
		return nil, nil, err
	}
	return secret, ciphertext, nil
}

func scanSecretMetadata(row interface{ Scan(...any) error }) (*secretv1.Secret, error) {
	return scanSecretMetadataWithCiphertext(row, nil)
}

func scanSecretMetadataWithCiphertext(row interface{ Scan(...any) error }, ciphertext *[]byte) (*secretv1.Secret, error) {
	var (
		id         string
		namespace  string
		typeText   string
		dataKeys   []byte
		labelsJSON []byte
		version    int64
		createdAt  time.Time
		updatedAt  time.Time
		payload    []byte
	)
	dest := []any{&id, &namespace, &typeText, &dataKeys, &labelsJSON, &version, &createdAt, &updatedAt}
	if ciphertext != nil {
		dest = append(dest, &payload)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	secret := &secretv1.Secret{
		ID:        id,
		Namespace: namespace,
		Type:      parseSecretType(typeText),
		Version:   version,
		CreatedAt: timestamppb.New(createdAt),
		UpdatedAt: timestamppb.New(updatedAt),
	}
	if err := json.Unmarshal(dataKeys, &secret.DataKeys); err != nil {
		return nil, fmt.Errorf("unmarshal secret data keys: %w", err)
	}
	if err := json.Unmarshal(labelsJSON, &secret.Labels); err != nil {
		return nil, fmt.Errorf("unmarshal secret labels: %w", err)
	}
	if ciphertext != nil {
		*ciphertext = payload
	}
	return secret, nil
}

func parseSecretType(value string) secretv1.SecretType {
	if n, ok := secretv1.SecretType_value[value]; ok {
		return secretv1.SecretType(n)
	}
	return secretv1.SecretType_SECRET_TYPE_UNSPECIFIED
}
