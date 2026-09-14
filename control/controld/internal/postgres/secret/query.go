package pgsecret

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/control/controld/internal/kernel/pagecursor"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
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

func (s *Store) List(ctx context.Context, filter *secretv1.SecretListFilter) ([]*secretv1.Secret, string, error) {
	if filter == nil {
		filter = &secretv1.SecretListFilter{}
	}
	cursor, err := pagecursor.Decode(filter.GetCursor())
	if err != nil {
		return nil, "", err
	}
	query := `
		SELECT secret_id, namespace, type, data_keys, labels, created_at
		FROM secrets
		WHERE TRUE`
	args := make([]any, 0, 6)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if namespace := strings.TrimSpace(filter.GetNamespace()); namespace != "" {
		query += ` AND namespace = ` + add(namespace)
	}
	if filter.GetType() != secretv1.SecretType_SECRET_TYPE_UNSPECIFIED {
		query += ` AND type = ` + add(filter.GetType().String())
	}
	if len(filter.GetLabels()) > 0 {
		labels, marshalErr := json.Marshal(filter.GetLabels())
		if marshalErr != nil {
			return nil, "", fmt.Errorf("marshal secret label filter: %w", marshalErr)
		}
		query += ` AND labels @> ` + add(string(labels)) + `::jsonb`
	}
	if !cursor.CreatedAt.IsZero() {
		query += ` AND (created_at, secret_id) < (` + add(cursor.CreatedAt) + `, ` + add(cursor.ID) + `)`
	}
	pageSize := pagecursor.PageSize(filter.GetPageSize())
	query += ` ORDER BY created_at DESC, secret_id DESC LIMIT ` + add(pageSize+1)
	rows, err := s.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query secrets: %w", err)
	}
	defer rows.Close()
	out := make([]*secretv1.Secret, 0)
	for rows.Next() {
		secret, err := scanSecretMetadata(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, secret)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(out) > pageSize {
		out = out[:pageSize]
		last := out[len(out)-1]
		nextCursor = pagecursor.Encode(last.GetCreatedAt().AsTime(), last.GetID())
	}
	return out, nextCursor, nil
}

func (s *Store) Delete(ctx context.Context, id string) (*secretv1.Secret, bool, error) {
	var deleted *secretv1.Secret
	err := withTx(ctx, s.db, func(tx pgx.Tx) error {
		record, _, err := getRecordTx(ctx, tx, strings.TrimSpace(id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM secrets WHERE secret_id = $1`, strings.TrimSpace(id)); err != nil {
			return fmt.Errorf("delete secret: %w", err)
		}
		deleted = record
		return nil
	})
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, false, nil
		}
		if postgres.IsForeignKeyViolation(err) {
			return nil, false, grpcstatus.Errorf(codes.FailedPrecondition, "secret %q is still referenced by an Environment or active Run", id)
		}
		return nil, false, err
	}
	return deleted, true, nil
}

func (s *Store) getRecord(ctx context.Context, id string) (*secretv1.Secret, []byte, error) {
	return getRecordTx(ctx, s.db.Pool(), id)
}
