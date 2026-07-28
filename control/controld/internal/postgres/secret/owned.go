package pgsecret

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateProfileCredentialTx persists a profile-owned credential in the caller's
// transaction. Internal credentials intentionally have no public Secret API
// representation.
func (s *Store) CreateProfileCredentialTx(ctx context.Context, tx pgx.Tx, namespace, profileID string, credential []byte, now time.Time) (string, int64, error) {
	value := strings.TrimSpace(string(credential))
	if value == "" {
		return "", 0, fmt.Errorf("profile credential is empty")
	}
	plaintext, err := json.Marshal(map[string]string{"token": value})
	if err != nil {
		return "", 0, fmt.Errorf("marshal profile credential: %w", err)
	}
	ciphertext, err := s.encrypt(plaintext)
	if err != nil {
		return "", 0, err
	}
	id := "sec-" + uuid.NewString()
	const version int64 = 1
	_, err = tx.Exec(ctx, `
		INSERT INTO secrets (
			secret_id, namespace, type, data_keys, encrypted_payload, labels,
			version, created_at, updated_at, visibility, owner_type, owner_id
		) VALUES ($1, $2, $3, '["token"]'::jsonb, $4, '{}'::jsonb,
			$5, $6, $6, 'INTERNAL', 'AGENT_PROFILE', $7)
	`, id, strings.TrimSpace(namespace), secretv1.SecretType_SECRET_TYPE_OPAQUE.String(), ciphertext, version, now.UTC(), profileID)
	if err != nil {
		return "", 0, fmt.Errorf("insert profile credential: %w", err)
	}
	return id, version, nil
}
