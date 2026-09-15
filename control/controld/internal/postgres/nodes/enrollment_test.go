package pgnodes

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	retentionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgretention "github.com/cofy-x/axern/control/controld/internal/postgres/retention"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type enrollmentTestSigner struct {
	calls atomic.Int32
	fail  bool
}

func (s *enrollmentTestSigner) SignNode([]byte, string, time.Time) ([]byte, error) {
	s.calls.Add(1)
	if s.fail {
		return nil, errors.New("signer unavailable")
	}
	return []byte("fixed-certificate"), nil
}

func TestEnrollmentTransactionReplayExpiryAndRetirement(t *testing.T) {
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	controldtest.ResetPostgresControlTables(t, dsn)
	ctx := context.Background()
	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	token := "random-test-enrollment-token-00000001"
	if _, err := db.Pool().Exec(ctx, "INSERT INTO nodes(node_id,node_target,enrollment_token_hash,admitted_at,lifecycle_status) VALUES('enrollment-node','',$1,$2,'active')", hashEnrollmentToken(token), now); err != nil {
		t.Fatal(err)
	}
	store := NewPGStore(db)
	request := nodekernel.EnrollmentRequest{NodeID: "enrollment-node", Token: token, CSRDER: []byte("verified-csr")}
	failing := &enrollmentTestSigner{fail: true}
	if _, err := store.Enroll(ctx, request, failing); err == nil {
		t.Fatal("signer failure accepted")
	}
	var count int
	if err := db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM node_enrollment_receipts").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed signing consumed token")
	}
	signer := &enrollmentTestSigner{}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			certificate, err := store.Enroll(ctx, request, signer)
			if err == nil && string(certificate) != "fixed-certificate" {
				err = errors.New("reply changed")
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if signer.calls.Load() != 1 {
		t.Fatalf("concurrent retries signed %d times", signer.calls.Load())
	}
	conflict := request
	conflict.CSRDER = []byte("different-key-csr")
	if _, err := store.Enroll(ctx, conflict, signer); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("different CSR replay=%v", err)
	}
	expired := request
	if _, err := db.Pool().Exec(ctx, "UPDATE nodes SET admitted_at=$1 WHERE node_id='enrollment-node'", now.Add(-2*nodekernel.EnrollmentLifetime)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enroll(ctx, expired, signer); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expired replay=%v", err)
	}
	result, err := pgretention.NewPGStore(db).Cleanup(ctx, retentionkernel.Config{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.EnrollmentReceiptsDeleted != 1 {
		t.Fatalf("receipt cleanup=%+v", result)
	}
	if _, err := store.Enroll(ctx, conflict, signer); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("receipt deletion reopened enrollment: %v", err)
	}
	if signer.calls.Load() != 1 {
		t.Fatal("expired token reached signer after retention")
	}
	if _, err := db.Pool().Exec(ctx, "UPDATE nodes SET lifecycle_status='retired',retired_at=$1,retired_reason='test retirement' WHERE node_id='enrollment-node'", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enroll(ctx, request, signer); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("retired Node replay=%v", err)
	}
}
