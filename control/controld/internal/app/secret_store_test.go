package app

import (
	"context"
	"strings"
	"testing"
	"time"

	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
)

func TestPostgresSecretStoreEncryptsPayloadAtRest(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	if app.secretDB == nil {
		t.Fatal("secretDB is nil")
	}
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	secret, err := app.secretDB.Create(context.Background(), secretkernel.CreateParams{
		Namespace:  "default",
		SecretType: secretv1.SecretType_SECRET_TYPE_OPAQUE,
		StringData: map[string]string{"token": "super-secret-value"},
	}, now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var encrypted []byte
	if err := app.db.Pool().QueryRow(context.Background(), `SELECT encrypted_payload FROM secrets WHERE secret_id = $1`, secret.GetID()).Scan(&encrypted); err != nil {
		t.Fatalf("query encrypted_payload: %v", err)
	}
	if len(encrypted) == 0 {
		t.Fatal("encrypted payload is empty")
	}
	if strings.Contains(string(encrypted), "super-secret-value") {
		t.Fatal("encrypted payload unexpectedly contains plaintext secret value")
	}

	resolved, ok, err := app.secretDB.Resolve(context.Background(), secret.GetID())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !ok {
		t.Fatal("Resolve() reported missing secret")
	}
	if resolved.Data["token"] != "super-secret-value" {
		t.Fatalf("resolved token = %q, want super-secret-value", resolved.Data["token"])
	}
}

func TestProfileCredentialCannotResolveThroughGenericSecretBoundary(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	tx, err := app.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := app.secretDB.CreateProfileCredentialTx(ctx, tx, "default", "apf-private", []byte("private-token"), now)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := app.secretDB.Resolve(ctx, id); err != nil || ok {
		t.Fatalf("generic Resolve() ok=%t err=%v, want hidden", ok, err)
	}
	resolved, ok, err := app.secretDB.ResolveProfileCredential(ctx, id)
	if err != nil || !ok || resolved.Data["token"] != "private-token" {
		t.Fatalf("profile credential resolve = %v ok=%t err=%v", resolved, ok, err)
	}
}
