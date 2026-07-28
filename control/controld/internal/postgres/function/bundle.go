package pgfunction

import (
	"context"
	"fmt"
	"strings"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) saveBundle(ctx context.Context, params functionkernel.UploadBundleParams, now time.Time) (*functionv1.FunctionBundleSource, error) {
	params.Namespace = functionkernel.NormalizeNamespace(params.Namespace)
	params.Name = functionkernel.NormalizeName(params.Name)
	bundle := functionkernel.NewBundleSource(params)
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := pgnamespace.Ensure(ctx, tx, params.Namespace); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO function_bundles (
				storage_uri, namespace, name, digest, media_type, size_bytes, payload, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (storage_uri) DO NOTHING
		`, bundle.GetStorageUri(), params.Namespace, params.Name, strings.TrimSpace(params.Digest), bundle.GetMediaType(), params.SizeBytes, params.Payload, now.UTC())
		if err != nil {
			return fmt.Errorf("save function bundle: %w", err)
		}
		if tag.RowsAffected() == 0 {
			if err := requireBundleTx(ctx, tx, bundle); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return bundle, nil
}

func requireBundleTx(ctx context.Context, tx pgx.Tx, bundle *functionv1.FunctionBundleSource) error {
	if bundle == nil {
		return nil
	}
	storageURI := strings.TrimSpace(bundle.GetStorageUri())
	if !strings.HasPrefix(storageURI, functionkernel.FunctionBundleStorageURIPrefix) {
		return grpcstatus.Error(codes.InvalidArgument, "function bundle storage_uri must come from UploadFunctionBundle")
	}
	var (
		digest    string
		mediaType string
		sizeBytes int64
	)
	if err := tx.QueryRow(ctx, `SELECT digest, media_type, size_bytes FROM function_bundles WHERE storage_uri = $1`, storageURI).Scan(&digest, &mediaType, &sizeBytes); err != nil {
		if err == pgx.ErrNoRows {
			return grpcstatus.Errorf(codes.FailedPrecondition, "function bundle %q has not been uploaded", storageURI)
		}
		return fmt.Errorf("get function bundle: %w", err)
	}
	if strings.TrimSpace(bundle.GetDigest()) != strings.TrimSpace(digest) {
		return grpcstatus.Errorf(codes.FailedPrecondition, "function bundle digest mismatch for %q", storageURI)
	}
	if strings.TrimSpace(bundle.GetMediaType()) != strings.TrimSpace(mediaType) {
		return grpcstatus.Errorf(codes.FailedPrecondition, "function bundle media_type mismatch for %q", storageURI)
	}
	if bundle.GetSizeBytes() != sizeBytes {
		return grpcstatus.Errorf(codes.FailedPrecondition, "function bundle size_bytes mismatch for %q", storageURI)
	}
	return nil
}

type BundlePayload struct {
	Digest    string
	MediaType string
	SizeBytes int64
	Payload   []byte
}

func (s *Store) ReadBundlePayload(ctx context.Context, storageURI string) (BundlePayload, bool, error) {
	storageURI = strings.TrimSpace(storageURI)
	if storageURI == "" {
		return BundlePayload{}, false, nil
	}
	var out BundlePayload
	if err := s.db.Pool().QueryRow(ctx, `
		SELECT digest, media_type, size_bytes, payload
		FROM function_bundles
		WHERE storage_uri = $1
	`, storageURI).Scan(&out.Digest, &out.MediaType, &out.SizeBytes, &out.Payload); err != nil {
		if err == pgx.ErrNoRows {
			return BundlePayload{}, false, nil
		}
		return BundlePayload{}, false, fmt.Errorf("read function bundle payload: %w", err)
	}
	return out, true, nil
}
