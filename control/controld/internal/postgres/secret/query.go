package pgsecret

import (
	"context"
	"fmt"
	"strings"

	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"github.com/jackc/pgx/v5"
)

func (s *Store) Get(ctx context.Context, id string) (*secretv1.Secret, bool, error) {
	secret, _, err := s.getRecord(ctx, strings.TrimSpace(id))
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return secret, true, nil
}

func (s *Store) List(ctx context.Context, filter *secretv1.SecretListFilter) ([]*secretv1.Secret, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT secret_id, namespace, type, data_keys, labels, version, created_at, updated_at
		FROM secrets
		WHERE visibility = 'PUBLIC'
		ORDER BY created_at DESC, secret_id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query secrets: %w", err)
	}
	defer rows.Close()
	out := make([]*secretv1.Secret, 0)
	for rows.Next() {
		secret, err := scanSecretMetadata(rows)
		if err != nil {
			return nil, err
		}
		if !matchFilter(secret, filter) {
			continue
		}
		out = append(out, secret)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) (*secretv1.Secret, bool, error) {
	var deleted *secretv1.Secret
	err := withTx(ctx, s.db, func(tx pgx.Tx) error {
		record, _, err := getRecordTx(ctx, tx, strings.TrimSpace(id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM secrets WHERE secret_id = $1 AND visibility = 'PUBLIC'`, strings.TrimSpace(id)); err != nil {
			return fmt.Errorf("delete secret: %w", err)
		}
		deleted = record
		return nil
	})
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return deleted, true, nil
}

func (s *Store) getRecord(ctx context.Context, id string) (*secretv1.Secret, []byte, error) {
	return getRecordTx(ctx, s.db.Pool(), id)
}
