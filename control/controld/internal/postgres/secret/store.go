package pgsecret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"time"

	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const dockerConfigJSONKey = ".dockerconfigjson"

type Store struct {
	db   *postgres.DB
	aead cipher.AEAD
}

func NewStore(db *postgres.DB, masterKey []byte) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &Store{db: db, aead: aead}, nil
}

func (s *Store) Create(ctx context.Context, params secretkernel.CreateParams, now time.Time) (*secretv1.Secret, error) {
	data, err := normalizeSecretData(params.SecretType, params.StringData)
	if err != nil {
		return nil, err
	}
	plaintext, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal secret payload: %w", err)
	}
	ciphertext, err := s.encrypt(plaintext)
	if err != nil {
		return nil, err
	}
	secret := &secretv1.Secret{
		ID:        "sec-" + uuid.NewString(),
		Namespace: normalizeNamespace(params.Namespace),
		Type:      params.SecretType,
		DataKeys:  sortedKeys(data),
		Labels:    cloneMap(params.Labels),
		CreatedAt: timestamppb.New(now),
	}
	dataKeysJSON, err := json.Marshal(secret.GetDataKeys())
	if err != nil {
		return nil, fmt.Errorf("marshal data keys: %w", err)
	}
	labelsJSON, err := json.Marshal(secret.GetLabels())
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create secret tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := pgnamespace.EnsureAt(ctx, tx, secret.GetNamespace(), now.UTC()); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO secrets (
			secret_id, namespace, type, data_keys, encrypted_payload, labels, created_at
		) VALUES ($1, $2, $3, $4::jsonb, $5, $6::jsonb, $7)
	`, secret.GetID(), secret.GetNamespace(), secret.GetType().String(), dataKeysJSON, ciphertext, labelsJSON, now.UTC()); err != nil {
		return nil, fmt.Errorf("insert secret: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create secret tx: %w", err)
	}
	return secret, nil
}

var (
	_ secretkernel.Control              = (*Store)(nil)
	_ secretkernel.MetadataReader       = (*Store)(nil)
	_ secretkernel.Mutator              = (*Store)(nil)
	_ secretkernel.ValueResolver        = (*Store)(nil)
	_ secretkernel.DockerConfigResolver = (*Store)(nil)
)
