package pgrollout

import (
	"context"
	"strings"
	"testing"
	"time"

	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ticketArtifactStore struct{}

func (ticketArtifactStore) PresignUpload(context.Context, string, string, int64, string, time.Duration) (rolloutkernel.ArtifactUpload, error) {
	panic("unexpected")
}
func (ticketArtifactStore) Verify(context.Context, string, int64, string) error { panic("unexpected") }
func (ticketArtifactStore) PresignDownload(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	return "https://object.internal/" + key + "?signed=redacted", time.Now().Add(ttl), nil
}
func (ticketArtifactStore) DeletePrefix(context.Context, string) error { panic("unexpected") }

func TestArtifactTicketBindsDurableArtifactState(t *testing.T) {
	db := newRolloutTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	clock := now
	store := NewStore(db, nil, nil, WithArtifactStore(ticketArtifactStore{}), WithArtifactTicketKey([]byte(strings.Repeat("k", 32))), WithNow(func() time.Time { return clock }))
	rollout, err := store.Create(ctx, rolloutkernel.CreateParams{Namespace: "default", StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO, Spec: &rolloutv1.RolloutSpec{TaskSetRef: "registry.example/tasks/test@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Agent: &rolloutv1.RolloutAgent{Name: "command", Command: "true"}, Execution: &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO rollout_artifacts(artifact_id,rollout_id,episode_id,execution_generation,kind,name,object_key,media_type,size_bytes,digest,status,created_at,committed_at) VALUES('art-test',$1,'',3,'evidence','result.json','rollouts/test/result.json','application/json',4,'sha256:abcd','ARTIFACT_STATUS_PRESENT',$2,$2)`, rollout.GetID(), now); err != nil {
		t.Fatal(err)
	}
	artifact, ticket, expires, found, err := store.PrepareArtifactDownload(ctx, "art-test", time.Minute)
	if err != nil || !found || artifact.GetID() != "art-test" || !expires.Equal(now.Add(time.Minute)) {
		t.Fatalf("PrepareArtifactDownload() = %v,%q,%v,%t,%v", artifact, ticket, expires, found, err)
	}
	resolved, url, headers, _, err := store.ResolveArtifactDownload(ctx, ticket, 2, time.Minute)
	if err != nil || resolved.GetID() != "art-test" || !strings.HasPrefix(url, "https://object.internal/") || headers["Range"] != "bytes=2-" {
		t.Fatalf("ResolveArtifactDownload() = %v,%q,%v,%v", resolved, url, headers, err)
	}
	if _, _, _, _, err := store.ResolveArtifactDownload(ctx, ticket+"x", 0, time.Minute); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("tampered ticket error = %v", err)
	}
	clock = now.Add(2 * time.Minute)
	if _, _, _, _, err := store.ResolveArtifactDownload(ctx, ticket, 0, time.Minute); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expired ticket error = %v", err)
	}
	clock = now
	if _, err := db.Pool().Exec(ctx, `UPDATE rollout_artifacts SET execution_generation=4 WHERE artifact_id='art-test'`); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := store.ResolveArtifactDownload(ctx, ticket, 0, time.Minute); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("generation mismatch error = %v", err)
	}
}

func TestListArtifactsRejectsUnknownRollout(t *testing.T) {
	store := NewStore(newRolloutTestDB(t), nil, nil)
	if _, err := store.ListArtifacts(context.Background(), "missing", ""); status.Code(err) != codes.NotFound {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
}
