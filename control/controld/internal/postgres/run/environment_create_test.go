package pgrun

import (
	"context"
	"os"
	"testing"
	"time"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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
	if _, err := store.DeleteEnvironment(ctx, first.GetID()); err != nil {
		t.Fatalf("DeleteEnvironment() error = %v", err)
	}

	third, err := store.CreateEnvironment(ctx, params, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("CreateEnvironment(third) error = %v", err)
	}
	if third.GetID() == first.GetID() || third.GetID() == second.GetID() {
		t.Fatalf("third create reused an existing environment ID %q", third.GetID())
	}
	if _, err := store.GetEnvironment(ctx, first.GetID()); grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("GetEnvironment(deleted) code = %s, want not found", grpcstatus.Code(err))
	}
}

func TestListEnvironmentsUsesStableKeysetAfterPhysicalDelete(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	params := runkernel.CreateEnvironmentParams{Spec: &environmentv1.EnvironmentSpec{Namespace: "default", TemplateID: "python311"}, Labels: map[string]string{"suite": "pagination"}}
	first, err := store.CreateEnvironment(ctx, params, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateEnvironment(ctx, params, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.CreateEnvironment(ctx, params, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteEnvironment(ctx, second.GetID()); err != nil {
		t.Fatal(err)
	}

	page, cursor, err := store.ListEnvironments(ctx, &environmentv1.ListFilter{Namespace: "default", Labels: map[string]string{"suite": "pagination"}, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].GetID() != third.GetID() || cursor == "" {
		t.Fatalf("first page = %#v cursor=%q", page, cursor)
	}
	tail, final, err := store.ListEnvironments(ctx, &environmentv1.ListFilter{Namespace: "default", Labels: map[string]string{"suite": "pagination"}, PageSize: 1, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 1 || final != "" || tail[0].GetID() != first.GetID() {
		t.Fatalf("second keyset page = %#v cursor=%q", tail, final)
	}
}

func TestDeleteEnvironmentIsPhysicalAndNotIdempotent(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	env, err := store.CreateEnvironment(ctx, runkernel.CreateEnvironmentParams{Spec: &environmentv1.EnvironmentSpec{Namespace: "default", TemplateID: "python311"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteEnvironment(ctx, env.GetID()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteEnvironment(ctx, env.GetID()); grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("second delete code = %s, want not found", grpcstatus.Code(err))
	}
}

func TestRunKeepsEnvironmentSnapshotAfterEnvironmentDelete(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	env, err := store.CreateEnvironment(ctx, runkernel.CreateEnvironmentParams{
		Spec:         &environmentv1.EnvironmentSpec{Namespace: "default", Image: &environmentv1.EnvironmentImageSource{Ref: "registry.example/runtime@sha256:source"}},
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{ImageDescriptor: &environmentv1.OciImageDescriptor{Digest: "sha256:resolved"}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	specJSON, err := marshalProtoJSON(env.GetSpec())
	if err != nil {
		t.Fatal(err)
	}
	resolvedJSON, err := marshalProtoJSON(env.GetResolvedSpec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO nodes (node_id, node_target, node_auth_token_hash, registered_at, last_heartbeat_at, lifecycle_status)
		VALUES ('node-snapshot', '127.0.0.1:24010', repeat('0', 64), $1, $1, 'active')
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, created_at, updated_at)
		VALUES ('run-snapshot', 'default', $2, 'RUN_STATUS_PLACED', '{}'::jsonb, $3::jsonb, $4::jsonb, '{}'::jsonb, $1, $1)
	`, now, env.GetID(), specJSON, resolvedJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, created_at, updated_at)
		VALUES ('alloc-snapshot', 'run-snapshot', 'node-snapshot', 'ALLOCATION_LIFECYCLE_STATE_BOUND', $1, $1)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteEnvironment(ctx, env.GetID()); err != nil {
		t.Fatal(err)
	}
	start, err := store.LoadStartAllocation(ctx, "alloc-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(start.Environment.GetSpec(), env.GetSpec()) || !proto.Equal(start.Environment.GetResolvedSpec(), env.GetResolvedSpec()) {
		t.Fatalf("start Environment = %#v, want admitted snapshot %#v", start.Environment, env)
	}
	if !proto.Equal(start.Run.GetEnvironmentSpec(), env.GetSpec()) || !proto.Equal(start.Run.GetResolvedEnvironmentSpec(), env.GetResolvedSpec()) {
		t.Fatalf("Run Environment snapshot = %#v / %#v", start.Run.GetEnvironmentSpec(), start.Run.GetResolvedEnvironmentSpec())
	}
}

func TestEnvironmentRegistrySecretReferenceIsRelational(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO secrets (secret_id, namespace, type, data_keys, encrypted_payload, labels, created_at)
		VALUES ('sec-registry', 'default', 'SECRET_TYPE_DOCKER_CONFIG_JSON', '[".dockerconfigjson"]'::jsonb, ''::bytea, '{}'::jsonb, $1)
	`, now); err != nil {
		t.Fatal(err)
	}
	env, err := store.CreateEnvironment(ctx, runkernel.CreateEnvironmentParams{
		Spec: &environmentv1.EnvironmentSpec{Namespace: "default", Image: &environmentv1.EnvironmentImageSource{Ref: "registry.example/runtime:latest", RegistryCredentialID: "sec-registry"}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM secrets WHERE secret_id = 'sec-registry'`); err == nil {
		t.Fatal("referenced registry Secret was deleted")
	}
	if _, err := store.DeleteEnvironment(ctx, env.GetID()); err != nil {
		t.Fatal(err)
	}
	if tag, err := db.Pool().Exec(ctx, `DELETE FROM secrets WHERE secret_id = 'sec-registry'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("delete unreferenced registry Secret rows=%d err=%v", tag.RowsAffected(), err)
	}
}

func TestRequiredRunSecretIDsIncludeEnvironmentCredentialAndExcludeOptionalReferences(t *testing.T) {
	got := requiredRunSecretIDs(&commonv1.ExecutionConfig{
		SecretEnv:   []*commonv1.SecretEnvVar{{SecretID: "sec-b"}, {SecretID: "sec-a"}, {SecretID: "sec-optional", Optional: true}},
		SecretFiles: []*commonv1.SecretFile{{SecretID: "sec-a"}},
	}, &environmentv1.EnvironmentSpec{Image: &environmentv1.EnvironmentImageSource{RegistryCredentialID: "sec-registry"}})
	if len(got) != 3 || got[0] != "sec-a" || got[1] != "sec-b" || got[2] != "sec-registry" {
		t.Fatalf("required Run Secret IDs = %v, want [sec-a sec-b sec-registry]", got)
	}
}

func newEnvironmentTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	controldtest.ResetPostgresControlTables(t, dsn)
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(db.Close)
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatalf("apply postgres migrations: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO namespaces (namespace, created_at) VALUES
			('default', now()), ('team-a', now()), ('team-b', now())
	`); err != nil {
		t.Fatalf("insert namespace fixtures: %v", err)
	}
	return db
}
