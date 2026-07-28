package postgres

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestStoreVolumeClassAndClaimCRUD(t *testing.T) {
	db := newStoreTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := testStoreNow()

	class := testVolumeClass("local", now)
	createdClass, err := store.CreateVolumeClass(ctx, class)
	if err != nil {
		t.Fatalf("CreateVolumeClass() error = %v", err)
	}
	if createdClass.GetName() != class.GetName() {
		t.Fatalf("created class name = %q, want %q", createdClass.GetName(), class.GetName())
	}
	gotClass, ok, err := store.GetVolumeClass(ctx, "local")
	if err != nil || !ok {
		t.Fatalf("GetVolumeClass() ok=%v err=%v", ok, err)
	}
	if gotClass.GetBackend() != storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL {
		t.Fatalf("class backend = %s, want local", gotClass.GetBackend())
	}
	classes, err := store.ListVolumeClasses(ctx)
	if err != nil {
		t.Fatalf("ListVolumeClasses() error = %v", err)
	}
	if len(classes) != 1 || classes[0].GetName() != "local" {
		t.Fatalf("classes = %#v, want local only", classes)
	}

	claim := testVolumeClaim("default", "data", "local", now)
	createdClaim, err := store.CreateVolumeClaim(ctx, claim)
	if err != nil {
		t.Fatalf("CreateVolumeClaim() error = %v", err)
	}
	if createdClaim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_PENDING {
		t.Fatalf("created claim status = %s, want pending", createdClaim.GetStatus())
	}
	updatedClaim, err := store.UpdateVolumeClaim(ctx, "default", "data", createdClaim.GetVersion(), func(next *storagev1.VolumeClaim) error {
		next.Labels = map[string]string{"tier": "hot"}
		next.Version++
		next.UpdatedAt = timestamppb.New(now.Add(time.Minute))
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateVolumeClaim() error = %v", err)
	}
	if updatedClaim.GetVersion() != 2 || updatedClaim.GetLabels()["tier"] != "hot" {
		t.Fatalf("updated claim version=%d labels=%v, want v2 tier=hot", updatedClaim.GetVersion(), updatedClaim.GetLabels())
	}
	claims, err := store.ListVolumeClaims(ctx, &storagev1.VolumeClaimListFilter{
		Namespace: "default",
		Statuses:  []storagev1.VolumeStatus{storagev1.VolumeStatus_VOLUME_STATUS_PENDING},
		Labels:    map[string]string{"tier": "hot"},
	})
	if err != nil {
		t.Fatalf("ListVolumeClaims() error = %v", err)
	}
	if len(claims) != 1 || claims[0].GetName() != "data" {
		t.Fatalf("claims = %#v, want default/data", claims)
	}
}

func TestStoreVolumeReclaimClaimsAreFencedAcrossWorkers(t *testing.T) {
	db := newStoreTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := testStoreNow()
	_, claim := createStoreClassAndClaim(t, ctx, store, now)
	claim, err := store.UpdateVolumeClaim(ctx, claim.GetNamespace(), claim.GetName(), claim.GetVersion(), func(next *storagev1.VolumeClaim) error {
		next.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETING
		next.ReclaimPolicy = storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE
		next.Topology = &storagev1.VolumeTopology{NodeID: "node-a"}
		next.OwnerType = "service"
		next.OwnerID = "svc-a"
		next.BackendHandle = next.GetID()
		next.Version++
		next.UpdatedAt = timestamppb.New(now)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make(chan []*privatestoragev1.VolumeReclaim, 2)
	errs := make(chan error, 2)
	for _, owner := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			got, claimErr := store.ClaimVolumeReclaims(ctx, &privatestoragev1.ClaimVolumeReclaimsRequest{Limit: 1, LeaseOwner: owner})
			results <- got
			errs <- claimErr
		}(owner)
	}
	wg.Wait()
	close(results)
	close(errs)
	for claimErr := range errs {
		if claimErr != nil {
			t.Fatalf("concurrent claim error = %v", claimErr)
		}
	}
	var first *privatestoragev1.VolumeReclaim
	claimed := 0
	for result := range results {
		claimed += len(result)
		if len(result) == 1 {
			first = result[0]
		}
	}
	if claimed != 1 || first.GetLeaseGeneration() != 1 || first.GetLeaseOwner() == "" || first.GetLeaseToken() == "" {
		t.Fatalf("concurrent claimed=%d first=%#v, want one generation-1 lease", claimed, first)
	}
	visible, ok, err := store.GetVolumeClaim(ctx, claim.GetNamespace(), claim.GetName())
	if err != nil || !ok || visible.GetReclaimLeaseToken() != "" {
		t.Fatalf("public claim ok=%v err=%v lease_token=%q", ok, err, visible.GetReclaimLeaseToken())
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE storage_volume_claims SET reclaim_lease_until=clock_timestamp()-interval '1 second' WHERE claim_id=$1`, claim.GetID()); err != nil {
		t.Fatal(err)
	}
	secondBatch, err := store.ClaimVolumeReclaims(ctx, &privatestoragev1.ClaimVolumeReclaimsRequest{Limit: 1, LeaseOwner: "worker-c"})
	if err != nil || len(secondBatch) != 1 || secondBatch[0].GetLeaseGeneration() != 2 {
		t.Fatalf("reclaimed batch=%#v err=%v, want generation 2", secondBatch, err)
	}
	if err := store.ReportVolumeReclaim(ctx, &privatestoragev1.ReportVolumeReclaimRequest{
		ClaimID: first.GetClaimID(), NodeID: first.GetNodeID(), Succeeded: true, LeaseToken: first.GetLeaseToken(),
		LeaseOwner: first.GetLeaseOwner(), LeaseGeneration: first.GetLeaseGeneration(),
	}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("late generation report error=%v, want stale fence", err)
	}
	second := secondBatch[0]
	if err := store.ReportVolumeReclaim(ctx, &privatestoragev1.ReportVolumeReclaimRequest{
		ClaimID: second.GetClaimID(), NodeID: second.GetNodeID(), Succeeded: true, LeaseToken: second.GetLeaseToken(),
		LeaseOwner: second.GetLeaseOwner(), LeaseGeneration: second.GetLeaseGeneration(),
	}); err != nil {
		t.Fatalf("current generation report error=%v", err)
	}
	deleted, err := store.ListVolumeClaims(ctx, &storagev1.VolumeClaimListFilter{Statuses: []storagev1.VolumeStatus{storagev1.VolumeStatus_VOLUME_STATUS_DELETED}})
	if err != nil || len(deleted) != 1 || deleted[0].GetID() != claim.GetID() {
		t.Fatalf("deleted claims=%#v err=%v", deleted, err)
	}
}

func TestStoreVolumeBindingPublishReleaseLifecycle(t *testing.T) {
	db := newStoreTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := testStoreNow()
	class, claim := createStoreClassAndClaim(t, ctx, store, now)

	volume, err := store.ReserveVolumeBinding(ctx, kernel.VolumeBindingReserve{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mount:        &privatestoragev1.WorkloadVolumeMount{ClaimName: "data", Target: "/data", Options: []string{"nodev"}},
		Claim:        claim,
		Class:        class,
	})
	if err != nil {
		t.Fatalf("ReserveVolumeBinding() error = %v", err)
	}
	if volume.GetBindingID() != "alloc-1/data" || volume.GetTopology().GetNodeID() != "node-a" {
		t.Fatalf("reserved volume binding=%q node=%q, want alloc-1/data on node-a", volume.GetBindingID(), volume.GetTopology().GetNodeID())
	}
	claimAfterReserve := getStoreClaim(t, ctx, store, "default", "data")
	if claimAfterReserve.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND || claimAfterReserve.GetTopology().GetNodeID() != "node-a" {
		t.Fatalf("claim after reserve status=%s topology=%v, want bound on node-a", claimAfterReserve.GetStatus(), claimAfterReserve.GetTopology())
	}

	binding, ok, err := store.GetVolumeBindingForClaim(ctx, claim.GetID())
	if err != nil || !ok {
		t.Fatalf("GetVolumeBindingForClaim() ok=%v err=%v", ok, err)
	}
	if binding.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND {
		t.Fatalf("binding status after reserve = %s, want bound", binding.GetStatus())
	}

	published := &privatestoragev1.PublishedNodeVolume{
		ClaimID:   volume.GetClaimID(),
		BindingID: volume.GetBindingID(),
		Backend:   volume.GetBackend(),
		HostPath:  "/var/lib/volumed/local/default/svc-123/data",
		Target:    volume.GetTarget(),
		Options:   append([]string(nil), volume.GetOptions()...),
	}
	if err := store.ReportVolumePublish(ctx, "alloc-1", "node-a", []*privatestoragev1.VolumePublishObservation{{
		BindingID:       volume.GetBindingID(),
		Status:          storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
		PublishedVolume: published,
	}}); err != nil {
		t.Fatalf("ReportVolumePublish() error = %v", err)
	}
	claimAfterPublish := getStoreClaim(t, ctx, store, "default", "data")
	if claimAfterPublish.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED {
		t.Fatalf("claim status after publish = %s, want published", claimAfterPublish.GetStatus())
	}
	reservedAgain, err := store.ReserveVolumeBinding(ctx, kernel.VolumeBindingReserve{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mount:        &privatestoragev1.WorkloadVolumeMount{ClaimName: "data", Target: "/data", Options: []string{"nodev"}},
		Claim:        claimAfterPublish,
		Class:        class,
	})
	if err != nil {
		t.Fatalf("ReserveVolumeBinding(idempotent published binding) error = %v", err)
	}
	if reservedAgain.GetBindingID() != volume.GetBindingID() {
		t.Fatalf("idempotent reserve binding = %q, want %q", reservedAgain.GetBindingID(), volume.GetBindingID())
	}
	bindingAfterReserve, ok, err := store.GetVolumeBindingForClaim(ctx, claim.GetID())
	if err != nil || !ok {
		t.Fatalf("GetVolumeBindingForClaim(after idempotent reserve) ok=%v err=%v", ok, err)
	}
	if bindingAfterReserve.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED {
		t.Fatalf("binding status after idempotent reserve = %s, want published", bindingAfterReserve.GetStatus())
	}

	if err := store.ReleaseVolumeBindings(ctx, "alloc-1", "node-a"); err != nil {
		t.Fatalf("ReleaseVolumeBindings() error = %v", err)
	}
	if err := store.ReleaseVolumeBindings(ctx, "alloc-1", "node-a"); err != nil {
		t.Fatalf("ReleaseVolumeBindings() second call error = %v", err)
	}
	if err := store.ReportVolumeRelease(ctx, "alloc-1", "node-a", []*privatestoragev1.VolumeReleaseObservation{{
		BindingID: volume.GetBindingID(),
		Status:    storagev1.VolumeStatus_VOLUME_STATUS_DELETED,
	}}); err != nil {
		t.Fatalf("ReportVolumeRelease(idempotent deleted observation) error = %v", err)
	}
	if err := store.ReportVolumeRelease(ctx, "alloc-1", "node-a", []*privatestoragev1.VolumeReleaseObservation{{
		BindingID: volume.GetBindingID(),
		Status:    storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
		Message:   "late unpublish failure",
	}}); err == nil || !strings.Contains(err.Error(), "already deleted") {
		t.Fatalf("ReportVolumeRelease(failed after deleted) error = %v, want already deleted", err)
	}
	activeBindings, err := store.ListVolumeBindings(ctx, kernel.VolumeBindingListFilter{})
	if err != nil {
		t.Fatalf("ListVolumeBindings(active) error = %v", err)
	}
	if len(activeBindings) != 0 {
		t.Fatalf("active bindings = %#v, want none after release", activeBindings)
	}
	deletedBindings, err := store.ListVolumeBindings(ctx, kernel.VolumeBindingListFilter{
		Statuses: []storagev1.VolumeStatus{storagev1.VolumeStatus_VOLUME_STATUS_DELETED},
	})
	if err != nil {
		t.Fatalf("ListVolumeBindings(deleted) error = %v", err)
	}
	if len(deletedBindings) != 1 || deletedBindings[0].GetBindingID() != volume.GetBindingID() {
		t.Fatalf("deleted bindings = %#v, want released binding", deletedBindings)
	}
	claimAfterRelease := getStoreClaim(t, ctx, store, "default", "data")
	if claimAfterRelease.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND {
		t.Fatalf("claim status after release = %s, want bound", claimAfterRelease.GetStatus())
	}
	if _, ok, err := store.GetVolumeBindingForClaim(ctx, claim.GetID()); err != nil || ok {
		t.Fatalf("GetVolumeBindingForClaim() after release ok=%v err=%v, want no active binding", ok, err)
	}
	if err := store.ReportVolumePublish(ctx, "alloc-1", "node-a", []*privatestoragev1.VolumePublishObservation{{
		BindingID:       volume.GetBindingID(),
		Status:          storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
		PublishedVolume: published,
	}}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ReportVolumePublish(after release) error = %v, want not found", err)
	}
}

func TestStoreRejectsVolumeBindingOnConflictingNode(t *testing.T) {
	db := newStoreTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := testStoreNow()
	class, claim := createStoreClassAndClaim(t, ctx, store, now)

	if _, err := store.ReserveVolumeBinding(ctx, kernel.VolumeBindingReserve{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mount:        &privatestoragev1.WorkloadVolumeMount{ClaimName: "data", Target: "/data"},
		Claim:        claim,
		Class:        class,
	}); err != nil {
		t.Fatalf("ReserveVolumeBinding() error = %v", err)
	}
	claim = getStoreClaim(t, ctx, store, "default", "data")
	if _, err := store.ReserveVolumeBinding(ctx, kernel.VolumeBindingReserve{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-2",
		NodeID:       "node-b",
		Mount:        &privatestoragev1.WorkloadVolumeMount{ClaimName: "data", Target: "/data"},
		Claim:        claim,
		Class:        class,
	}); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("ReserveVolumeBinding(conflicting node) error = %v, want already bound", err)
	}
}

func TestStorePreservesDeletedClaimTombstoneAndRecreatesName(t *testing.T) {
	db := newStoreTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	if _, err := store.CreateVolumeClass(ctx, testVolumeClass("local", now)); err != nil {
		t.Fatal(err)
	}
	oldClaim := testVolumeClaim("default", "agent-workspace-project-a", "local", now)
	oldClaim.ID = "claim-old"
	if _, err := store.CreateVolumeClaim(ctx, oldClaim); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateVolumeClaim(ctx, oldClaim.GetNamespace(), oldClaim.GetName(), oldClaim.GetVersion(), func(claim *storagev1.VolumeClaim) error {
		claim.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETED
		claim.Version++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	newClaim := testVolumeClaim("default", oldClaim.GetName(), "local", now.Add(time.Second))
	newClaim.ID = "claim-new"
	if _, err := store.CreateVolumeClaim(ctx, newClaim); err != nil {
		t.Fatal(err)
	}
	active := getStoreClaim(t, ctx, store, "default", oldClaim.GetName())
	if active.GetID() != "claim-new" {
		t.Fatalf("active claim id = %q", active.GetID())
	}
	tombstones, err := store.ListVolumeClaims(ctx, &storagev1.VolumeClaimListFilter{Statuses: []storagev1.VolumeStatus{storagev1.VolumeStatus_VOLUME_STATUS_DELETED}})
	if err != nil || len(tombstones) != 1 || tombstones[0].GetID() != "claim-old" {
		t.Fatalf("tombstones = %#v, err=%v", tombstones, err)
	}
}

func TestStoreReportsFailedPublishAndAllowsRetry(t *testing.T) {
	db := newStoreTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := testStoreNow()
	class, claim := createStoreClassAndClaim(t, ctx, store, now)

	volume, err := store.ReserveVolumeBinding(ctx, kernel.VolumeBindingReserve{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mount:        &privatestoragev1.WorkloadVolumeMount{ClaimName: "data", Target: "/data"},
		Claim:        claim,
		Class:        class,
	})
	if err != nil {
		t.Fatalf("ReserveVolumeBinding() error = %v", err)
	}
	if err := store.ReportVolumePublish(ctx, "alloc-1", "node-a", []*privatestoragev1.VolumePublishObservation{{
		BindingID: volume.GetBindingID(),
		Status:    storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
		Message:   "mount failed",
	}}); err != nil {
		t.Fatalf("ReportVolumePublish(failed) error = %v", err)
	}
	claimAfterFailure := getStoreClaim(t, ctx, store, "default", "data")
	if claimAfterFailure.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_FAILED || claimAfterFailure.GetMessage() != "mount failed" {
		t.Fatalf("claim after failure status=%s message=%q, want failed mount failed", claimAfterFailure.GetStatus(), claimAfterFailure.GetMessage())
	}
	retry, err := store.RetryFailedVolumeBinding(ctx, volume.GetBindingID(), "node recovered")
	if err != nil {
		t.Fatalf("RetryFailedVolumeBinding() error = %v", err)
	}
	if retry.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND {
		t.Fatalf("retry binding status = %s, want bound", retry.GetStatus())
	}
	claimAfterRetryAction := getStoreClaim(t, ctx, store, "default", "data")
	if claimAfterRetryAction.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND {
		t.Fatalf("claim after retry action status = %s, want bound", claimAfterRetryAction.GetStatus())
	}
	claim = getStoreClaim(t, ctx, store, "default", "data")
	if _, err := store.ReserveVolumeBinding(ctx, kernel.VolumeBindingReserve{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mount:        &privatestoragev1.WorkloadVolumeMount{ClaimName: "data", Target: "/data"},
		Claim:        claim,
		Class:        class,
	}); err != nil {
		t.Fatalf("ReserveVolumeBinding(retry same allocation) error = %v", err)
	}
	claimAfterRetry := getStoreClaim(t, ctx, store, "default", "data")
	if claimAfterRetry.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND {
		t.Fatalf("claim after retry reserve status = %s, want bound", claimAfterRetry.GetStatus())
	}
}

func newStoreTestDB(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(db.Close)
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatalf("apply storaged postgres migrations: %v", err)
	}
	truncateStoreTestTables(t, db)
	return db
}

func truncateStoreTestTables(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		TRUNCATE TABLE
			storage_volume_bindings,
			storage_volume_claims,
			storage_volume_classes
		CASCADE
	`); err != nil {
		t.Fatalf("truncate storaged test tables: %v", err)
	}
}

func createStoreClassAndClaim(t *testing.T, ctx context.Context, store *Store, now time.Time) (*storagev1.VolumeClass, *storagev1.VolumeClaim) {
	t.Helper()
	class, err := store.CreateVolumeClass(ctx, testVolumeClass("local", now))
	if err != nil {
		t.Fatalf("CreateVolumeClass() error = %v", err)
	}
	claim, err := store.CreateVolumeClaim(ctx, testVolumeClaim("default", "data", "local", now))
	if err != nil {
		t.Fatalf("CreateVolumeClaim() error = %v", err)
	}
	return class, claim
}

func getStoreClaim(t *testing.T, ctx context.Context, store *Store, namespace, name string) *storagev1.VolumeClaim {
	t.Helper()
	claim, ok, err := store.GetVolumeClaim(ctx, namespace, name)
	if err != nil || !ok {
		t.Fatalf("GetVolumeClaim(%s/%s) ok=%v err=%v", namespace, name, ok, err)
	}
	return claim
}

func testVolumeClass(name string, now time.Time) *storagev1.VolumeClass {
	return &storagev1.VolumeClass{
		Name:                 name,
		Backend:              storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		AccessModes:          []storagev1.VolumeAccessMode{storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE},
		DefaultReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		ConsistencyProfile:   storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{SupportsRunc: true, SupportsRunsc: true},
		CreatedAt:            timestamppb.New(now),
		UpdatedAt:            timestamppb.New(now),
	}
}

func testVolumeClaim(namespace, name, className string, now time.Time) *storagev1.VolumeClaim {
	return &storagev1.VolumeClaim{
		ID:                namespace + "/" + name,
		Namespace:         namespace,
		Name:              name,
		ClassName:         className,
		RequestedCapacity: &storagev1.VolumeCapacity{SizeBytes: 1024},
		AccessMode:        storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		ReclaimPolicy:     storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		BindingScope:      storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
		Status:            storagev1.VolumeStatus_VOLUME_STATUS_PENDING,
		Version:           1,
		CreatedAt:         timestamppb.New(now),
		UpdatedAt:         timestamppb.New(now),
	}
}

func testStoreNow() time.Time {
	return time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
}
