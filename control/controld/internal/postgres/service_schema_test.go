package postgres

import (
	"context"
	"os"
	"testing"
)

func TestServiceDeletionStatusSchemaContract(t *testing.T) {
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatalf("ApplyMigrations() error = %v", err)
	}
	tx, err := db.Pool().Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(context.Background(), `
		INSERT INTO services (
			service_id, namespace, environment_id, replicas,
			status, config, allocation_ids, labels, created_at, updated_at
		) VALUES (
			'svc-deletion-schema', 'default', 'env-test', 0,
			'SERVICE_STATUS_READY', '{}'::jsonb, '[]'::jsonb, '{}'::jsonb, now(), now()
		)
	`); err != nil {
		t.Fatalf("insert service with default deletion status: %v", err)
	}
	var deletionStatusIsJSONNull bool
	if err := tx.QueryRow(context.Background(), `
		SELECT deletion_status = 'null'::jsonb
		FROM services WHERE service_id = 'svc-deletion-schema'
	`).Scan(&deletionStatusIsJSONNull); err != nil {
		t.Fatalf("query default deletion status: %v", err)
	}
	if !deletionStatusIsJSONNull {
		t.Fatal("default deletion status is not JSON null")
	}
	if _, err := tx.Exec(context.Background(), `
		UPDATE services SET deletion_status = NULL WHERE service_id = 'svc-deletion-schema'
	`); err == nil {
		t.Fatal("setting deletion status to SQL NULL succeeded, want NOT NULL violation")
	}
}
