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
	if deleted.GetDeletedAt() == nil {
		t.Fatal("deleted environment has no deletion timestamp")
	}
}

func TestListEnvironmentsUsesStableKeysetAndExcludesDeletedByDefault(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	params := runkernel.CreateEnvironmentParams{Spec: &environmentv1.EnvironmentSpec{Namespace: "default", TemplateID: "python311"}, Labels: map[string]string{"suite": "pagination"}}
	first, err := store.CreateEnvironment(ctx, params, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateEnvironment(ctx, params, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteEnvironment(ctx, second.GetID(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	page, cursor, err := store.ListEnvironments(ctx, &environmentv1.ListFilter{Namespace: "default", Labels: map[string]string{"suite": "pagination"}, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].GetID() != first.GetID() || cursor != "" {
		t.Fatalf("live page = %#v cursor=%q", page, cursor)
	}
	all, next, err := store.ListEnvironments(ctx, &environmentv1.ListFilter{Namespace: "default", Labels: map[string]string{"suite": "pagination"}, IncludeDeleted: true, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || next == "" {
		t.Fatalf("first keyset page returned %d environments, cursor=%q", len(all), next)
	}
	tail, final, err := store.ListEnvironments(ctx, &environmentv1.ListFilter{Namespace: "default", Labels: map[string]string{"suite": "pagination"}, IncludeDeleted: true, PageSize: 1, Cursor: next})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 1 || final != "" || tail[0].GetID() == all[0].GetID() {
		t.Fatalf("second keyset page = %#v cursor=%q", tail, final)
	}
}

func TestDeleteEnvironmentPreservesFirstTombstoneTimestamp(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	env, err := store.CreateEnvironment(ctx, runkernel.CreateEnvironmentParams{Spec: &environmentv1.EnvironmentSpec{Namespace: "default", TemplateID: "python311"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.DeleteEnvironment(ctx, env.GetID(), now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.DeleteEnvironment(ctx, env.GetID(), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !first.GetDeletedAt().AsTime().Equal(second.GetDeletedAt().AsTime()) {
		t.Fatalf("delete timestamp changed from %v to %v", first.GetDeletedAt(), second.GetDeletedAt())
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
