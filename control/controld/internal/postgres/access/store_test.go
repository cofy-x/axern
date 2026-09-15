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
	"github.com/cofy-x/axern/control/controld/internal/postgres"
)

func TestBootstrapResolveAndLastAdministratorGuard(t *testing.T) {
	db := newAccessTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	fingerprint := sha256.Sum256([]byte("platform-admin-certificate"))
	if err := store.BootstrapPlatformAdmin(ctx, "platform-admin", "Platform Administrator", "bootstrap", []accesskernel.CredentialMaterial{{Kind: accesskernel.CredentialX509, Fingerprint: fingerprint, ExpiresAt: now.Add(24 * time.Hour)}}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.BootstrapPlatformAdmin(ctx, "platform-admin", "Platform Administrator", "bootstrap", []accesskernel.CredentialMaterial{{Kind: accesskernel.CredentialX509, Fingerprint: fingerprint, ExpiresAt: now.Add(24 * time.Hour)}}, now); err != nil {
		t.Fatalf("exact bootstrap retry: %v", err)
	}
	if err := store.BootstrapPlatformAdmin(ctx, "platform-admin", "Different", "bootstrap", []accesskernel.CredentialMaterial{{Kind: accesskernel.CredentialX509, Fingerprint: fingerprint, ExpiresAt: now.Add(24 * time.Hour)}}, now); err == nil {
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

func TestBootstrapSSHCredentialIsAtomicAndNeverReactivated(t *testing.T) {
	db := newAccessTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	credentials := []accesskernel.CredentialMaterial{
		{Kind: accesskernel.CredentialX509, Fingerprint: sha256.Sum256([]byte("bootstrap certificate")), ExpiresAt: now.Add(time.Hour)},
		{Kind: accesskernel.CredentialSSH, Fingerprint: sha256.Sum256([]byte("bootstrap SSH key")), ExpiresAt: now.Add(time.Hour)},
	}
	bootstrap := func(values []accesskernel.CredentialMaterial) error {
		return store.BootstrapPlatformAdmin(ctx, "admin", "Administrator", "bootstrap", values, now)
	}
	if err := bootstrap(append(credentials, credentials[1])); err == nil {
		t.Fatal("duplicate credential accepted")
	}
	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM principals`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial bootstrap persisted: %d %v", count, err)
	}
	if err := bootstrap(credentials); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(credentials); err != nil {
		t.Fatalf("exact retry: %v", err)
	}
	actor, err := store.ResolveActor(ctx, credentials[1].Fingerprint, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeCredential(ctx, actor.Principal.ID, actor.Credential.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(credentials); err == nil {
		t.Fatal("bootstrap reactivated revoked SSH key")
	}
	if _, err := store.ResolveActor(ctx, credentials[1].Fingerprint, now); !errors.Is(err, accesskernel.ErrUnauthenticated) {
		t.Fatalf("revoked key resolved: %v", err)
	}
}

func TestGrantNamespaceBindingRequiresExistingNamespace(t *testing.T) {
	db := newAccessTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	adminFingerprint := sha256.Sum256([]byte("platform-admin-certificate"))
	if err := store.BootstrapPlatformAdmin(ctx, "platform-admin", "Platform Administrator", "bootstrap", []accesskernel.CredentialMaterial{{Kind: accesskernel.CredentialX509, Fingerprint: adminFingerprint, ExpiresAt: now.Add(24 * time.Hour)}}, now); err != nil {
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
	if _, err := db.Pool().Exec(ctx, `INSERT INTO namespaces(namespace,created_at) VALUES('team-a',$1)`, now); err != nil {
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

func TestSSHCredentialSharesPrincipalRevocationButCannotReplaceAdminCertificate(t *testing.T) {
	db := newAccessTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Now().UTC()
	fingerprint := sha256.Sum256([]byte("admin-certificate"))
	if err := store.BootstrapPlatformAdmin(ctx, "admin", "Administrator", "bootstrap", []accesskernel.CredentialMaterial{{Kind: accesskernel.CredentialX509, Fingerprint: fingerprint, ExpiresAt: now.Add(time.Hour)}}, now); err != nil {
		t.Fatal(err)
	}
	actor, err := store.ResolveActor(ctx, fingerprint, now)
	if err != nil {
		t.Fatal(err)
	}
	sshHash := sha256.Sum256([]byte("ssh-wire-public-key"))
	credential, err := store.AddCredential(ctx, actor.Principal.ID, actor.Principal.ID, "ssh", accesskernel.CredentialMaterial{Kind: accesskernel.CredentialSSH, Fingerprint: sshHash, ExpiresAt: now.Add(time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	sshActor, err := store.ResolveActor(ctx, sshHash, now)
	if err != nil || sshActor.Principal.ID != actor.Principal.ID || sshActor.Credential.Kind != accesskernel.CredentialSSH {
		t.Fatalf("SSH actor: %+v, %v", sshActor, err)
	}
	if _, err := store.RevokeCredential(ctx, actor.Principal.ID, actor.Credential.ID, now); !errors.Is(err, accesskernel.ErrFailedPrecondition) {
		t.Fatalf("removed last management credential: %v", err)
	}
	if _, err := store.RevokeCredential(ctx, actor.Principal.ID, credential.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveActor(ctx, sshHash, now); !errors.Is(err, accesskernel.ErrUnauthenticated) {
		t.Fatalf("revoked SSH credential resolved: %v", err)
	}
}
