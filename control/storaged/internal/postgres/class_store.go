package postgres

import (
	"context"
	"fmt"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

func (s *Store) CreateVolumeClass(ctx context.Context, class *storagev1.VolumeClass) (*storagev1.VolumeClass, error) {
	payload, err := marshalProto(class)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Pool().Exec(ctx, `
		INSERT INTO storage_volume_classes (name, backend, payload, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, $5)
	`, class.GetName(), class.GetBackend().String(), payload, class.GetCreatedAt().AsTime(), class.GetUpdatedAt().AsTime())
	if err != nil {
		return nil, fmt.Errorf("create volume class %q: %w", class.GetName(), err)
	}
	return proto.Clone(class).(*storagev1.VolumeClass), nil
}

func (s *Store) GetVolumeClass(ctx context.Context, name string) (*storagev1.VolumeClass, bool, error) {
	row := s.db.Pool().QueryRow(ctx, `SELECT payload FROM storage_volume_classes WHERE name = $1`, strings.TrimSpace(name))
	out, err := scanVolumeClass(row)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (s *Store) ListVolumeClasses(ctx context.Context) ([]*storagev1.VolumeClass, error) {
	rows, err := s.db.Pool().Query(ctx, `SELECT payload FROM storage_volume_classes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storagev1.VolumeClass
	for rows.Next() {
		class, err := scanVolumeClass(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, class)
	}
	return out, rows.Err()
}
