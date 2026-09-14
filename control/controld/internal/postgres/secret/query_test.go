package pgsecret

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
)

func TestListFiltersAndPaginatesInDatabase(t *testing.T) {
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE secrets`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO namespaces(namespace, created_at) VALUES ('team-a',$1),('team-b',$1) ON CONFLICT DO NOTHING`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO secrets (secret_id, namespace, type, data_keys, encrypted_payload, labels, created_at) VALUES
		('sec-page-a', 'team-a', 'SECRET_TYPE_OPAQUE', '[]'::jsonb, ''::bytea, '{"suite":"page"}'::jsonb, $1),
		('sec-page-b', 'team-a', 'SECRET_TYPE_OPAQUE', '[]'::jsonb, ''::bytea, '{"suite":"page"}'::jsonb, $1),
		('sec-page-c', 'team-b', 'SECRET_TYPE_OPAQUE', '[]'::jsonb, ''::bytea, '{"suite":"page"}'::jsonb, $1)
	`, now); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	filter := &secretv1.SecretListFilter{Namespace: "team-a", Type: secretv1.SecretType_SECRET_TYPE_OPAQUE, Labels: map[string]string{"suite": "page"}, PageSize: 1}
	first, cursor, err := store.List(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || cursor == "" {
		t.Fatalf("first page = %#v cursor=%q", first, cursor)
	}
	filter.Cursor = cursor
	second, final, err := store.List(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || final != "" || second[0].GetID() == first[0].GetID() {
		t.Fatalf("second page = %#v cursor=%q", second, final)
	}
}
