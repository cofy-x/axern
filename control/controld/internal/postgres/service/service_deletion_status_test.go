package pgservice

import (
	"context"
	"os"
	"testing"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The deletion lifecycle is driven from the persisted service row: every
// reconcile re-reads the row and must see the same deletion status that the
// previous write recorded. This round trip is what keeps workspace deletion
// waiting for physical volume reclaim instead of completing silently.
func TestServiceDeletionStatusRoundTrip(t *testing.T) {
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if _, err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply postgres migrations: %v", err)
	}

	store := NewPGStore(db)
	now := time.Now().UTC()
	created, err := store.Create(ctx, servicekernel.CreateParams{
		Namespace:     "default",
		EnvironmentID: "env-deletion-roundtrip",
		Replicas:      0,
		Config:        &commonv1.ExecutionConfig{},
	}, now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	serviceID := created.GetID()

	deleted, ok, err := store.Delete(ctx, servicekernel.DeleteParams{
		ServiceID:         serviceID,
		VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE,
	}, now.Add(time.Second))
	if err != nil || !ok {
		t.Fatalf("Delete() = ok %v, err %v", ok, err)
	}
	if deleted.GetDeletionStatus().GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS {
		t.Fatalf("Delete() deletion status = %#v", deleted.GetDeletionStatus())
	}

	reloaded, ok, err := store.Get(ctx, serviceID)
	if err != nil || !ok {
		t.Fatalf("Get() = ok %v, err %v", ok, err)
	}
	deletion := reloaded.GetDeletionStatus()
	if deletion == nil {
		t.Fatal("Get() deletion status = nil after Delete(), want persisted RELEASING_ALLOCATIONS")
	}
	if deletion.GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS ||
		deletion.GetVolumeDisposition() != servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE {
		t.Fatalf("Get() deletion status = %#v", deletion)
	}

	if _, err := store.UpdateDeletionStatus(ctx, serviceID, &servicev1.ServiceDeletionStatus{
		Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE,
		VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE,
		ClaimIds:          []string{"claim-roundtrip"},
		Message:           "service deletion complete",
		CompletedAt:       timestamppb.New(now.Add(2 * time.Second)),
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("UpdateDeletionStatus() error = %v", err)
	}

	reloaded, ok, err = store.Get(ctx, serviceID)
	if err != nil || !ok {
		t.Fatalf("Get() after completion = ok %v, err %v", ok, err)
	}
	deletion = reloaded.GetDeletionStatus()
	if reloaded.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		t.Fatalf("Get() status = %v, want DELETED", reloaded.GetStatus())
	}
	if deletion.GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE ||
		len(deletion.GetClaimIds()) != 1 || deletion.GetClaimIds()[0] != "claim-roundtrip" ||
		deletion.GetCompletedAt() == nil {
		t.Fatalf("Get() completed deletion status = %#v", deletion)
	}
}
