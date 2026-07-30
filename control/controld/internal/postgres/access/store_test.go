package access

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
)

func TestBootstrapResolveAndLastAdministratorGuard(t *testing.T) {
	db := newAccessTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	fingerprint := sha256.Sum256([]byte("platform-admin-certificate"))
	if err := store.BootstrapPlatformAdmin(ctx, "platform-admin", "Platform Administrator", "bootstrap", fingerprint, now.Add(24*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if err := store.BootstrapPlatformAdmin(ctx, "platform-admin", "Platform Administrator", "bootstrap", fingerprint, now.Add(24*time.Hour), now); err != nil {
		t.Fatalf("exact bootstrap retry: %v", err)
	}
	if err := store.BootstrapPlatformAdmin(ctx, "platform-admin", "Different", "bootstrap", fingerprint, now.Add(24*time.Hour), now); err == nil {
		t.Fatal("mismatched bootstrap succeeded")
	}
	actor, err := store.ResolveActor(ctx, fingerprint, now)
	if err != nil || !accesskernel.HasRole(actor, accesskernel.RolePlatformAdmin) {
		t.Fatalf("ResolveActor() actor=%+v err=%v", actor, err)
	}
	bindings, err := store.ListBindings(ctx, actor.Principal.ID, "", false)
	if err != nil || len(bindings) != 1 {
		t.Fatalf("ListBindings()=%v,%v", bindings, err)
	}
	if _, err := store.RevokeBinding(ctx, actor.Principal.ID, bindings[0].ID, now); err == nil || !strings.Contains(err.Error(), "last active platform administrator") {
		t.Fatalf("RevokeBinding(last admin)=%v", err)
	}
}

func TestValidateRolloutExecutionLeaseIsNamespaceAndExpiryScoped(t *testing.T) {
	db := newAccessTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO namespaces(namespace,version,created_at,updated_at) VALUES ('team-a',1,$1,$1),('team-b',1,$1,$1)`, []any{now}},
		{`INSERT INTO rollouts(rollout_id,namespace,status,start_policy,spec,spec_hash,labels,version,created_at) VALUES ('rol-access','team-a','ROLLOUT_STATUS_RUNNING','ROLLOUT_START_POLICY_AUTO','{}','hash','{}',1,$1)`, []any{now}},
		{`INSERT INTO rollout_episodes(episode_id,rollout_id,task_id,task_digest,attempt_index,execution_generation,status,created_at) VALUES ('ep-access','rol-access','task','digest',1,1,'EPISODE_STATUS_LEASED',$1)`, []any{now}},
		{`INSERT INTO rollout_work_items(work_id,kind,rollout_id,episode_id,execution_generation,status,next_run_at,claim_owner,lease_token_hash,lease_expires_at,created_at,updated_at) VALUES ('wrk-access','WORK_KIND_EPISODE','rol-access','ep-access',1,'LEASED',$1,'worker',$2,$3,$1,$1)`, []any{now, rolloutkernel.HashToken("lease-token"), now.Add(time.Minute)}},
	}
	for _, statement := range statements {
		if _, err := db.Pool().Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ValidateRolloutExecutionLease(ctx, "lease-token", "team-a", now); err != nil {
		t.Fatalf("valid lease: %v", err)
	}
	if err := store.ValidateRolloutExecutionLease(ctx, "lease-token", "team-b", now); err == nil {
		t.Fatal("cross-namespace lease succeeded")
	}
	if err := store.ValidateRolloutExecutionLease(ctx, "lease-token", "team-a", now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired lease succeeded")
	}
}

func TestGrantNamespaceBindingRequiresExistingNamespace(t *testing.T) {
	db := newAccessTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	adminFingerprint := sha256.Sum256([]byte("platform-admin-certificate"))
	if err := store.BootstrapPlatformAdmin(ctx, "platform-admin", "Platform Administrator", "bootstrap", adminFingerprint, now.Add(24*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	admin, err := store.ResolveActor(ctx, adminFingerprint, now)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := store.CreatePrincipal(ctx, admin.Principal.ID, "developer", "Developer", accesskernel.PrincipalKindHuman, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantBinding(ctx, admin.Principal.ID, principal.ID, accesskernel.ScopeNamespace, "missing", accesskernel.RoleNamespaceViewer, now); !errors.Is(err, accesskernel.ErrNotFound) {
		t.Fatalf("GrantBinding(missing namespace)=%v, want not found", err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO namespaces(namespace,version,created_at,updated_at) VALUES('team-a',1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantBinding(ctx, admin.Principal.ID, principal.ID, accesskernel.ScopeNamespace, "team-a", accesskernel.RoleNamespaceViewer, now); err != nil {
		t.Fatalf("GrantBinding(existing namespace)=%v", err)
	}
}

func newAccessTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE principals,namespaces CASCADE`); err != nil {
		t.Fatal(err)
	}
	return db
}
