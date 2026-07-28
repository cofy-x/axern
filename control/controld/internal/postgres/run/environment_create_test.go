package pgrun

import (
	"context"
	"os"
	"testing"
	"time"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

func TestCreateEnvironmentAlwaysCreatesOwnedResource(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	params := runkernel.CreateEnvironmentParams{
		Spec: &environmentv1.EnvironmentSpec{Namespace: "default", TemplateID: "python311"},
	}

	first, err := store.CreateEnvironment(ctx, params, now)
	if err != nil {
		t.Fatalf("CreateEnvironment(first) error = %v", err)
	}
	second, err := store.CreateEnvironment(ctx, params, now.Add(time.Second))
	if err != nil {
		t.Fatalf("CreateEnvironment(second) error = %v", err)
	}
	if second.GetID() == first.GetID() {
		t.Fatalf("independent creates shared environment ID %q", first.GetID())
	}
	if _, err := store.DeleteEnvironment(ctx, first.GetID(), now.Add(2*time.Second)); err != nil {
		t.Fatalf("DeleteEnvironment() error = %v", err)
	}

	third, err := store.CreateEnvironment(ctx, params, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("CreateEnvironment(third) error = %v", err)
	}
	if third.GetID() == first.GetID() || third.GetID() == second.GetID() {
		t.Fatalf("third create reused an existing environment ID %q", third.GetID())
	}
	deleted, err := store.GetEnvironment(ctx, first.GetID())
	if err != nil {
		t.Fatalf("GetEnvironment(deleted) error = %v", err)
	}
	if deleted.GetStatus() != environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_DELETED {
		t.Fatalf("deleted environment status = %s", deleted.GetStatus())
	}
}

func newEnvironmentTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(db.Close)
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatalf("apply postgres migrations: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE environments CASCADE`); err != nil {
		t.Fatalf("truncate environments: %v", err)
	}
	return db
}
