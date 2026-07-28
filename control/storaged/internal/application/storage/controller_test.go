package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/storaged/internal/storetest"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestWorkloadClaimReclaimRetriesAndAllowsSameNameRecreate(t *testing.T) {
	ctx := context.Background()
	store := storetest.NewMemoryStore()
	controller := NewController(store)
	now := time.Unix(100, 0).UTC()
	controller.now = func() time.Time { return now }
	mustCreateLocalClass(t, ctx, controller)
	created, err := controller.CreateVolumeClaim(ctx, &storagev1.CreateVolumeClaimRequest{
		Namespace: "default", Name: "agent-workspace-project-a", ClassName: "local",
		AccessMode:    storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		ReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE,
		BindingScope:  storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err = store.UpdateVolumeClaim(ctx, created.GetNamespace(), created.GetName(), created.GetVersion(), func(claim *storagev1.VolumeClaim) error {
		claim.OwnerType = "service"
		claim.OwnerID = "svc-1"
		claim.Topology = &storagev1.VolumeTopology{NodeID: "node-a"}
		claim.Version++
		claim.UpdatedAt = timestamppb.New(now)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := controller.DeleteWorkloadVolumeClaims(ctx, &privatestoragev1.DeleteWorkloadVolumeClaimsRequest{Namespace: "default", WorkloadID: "svc-1"})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetComplete() || len(response.GetReclaims()) != 0 {
		t.Fatalf("initial reclaim = %#v", response)
	}
	claimed, err := controller.ClaimVolumeReclaims(ctx, &privatestoragev1.ClaimVolumeReclaimsRequest{Limit: 1, LeaseOwner: "worker-a"})
	if err != nil || len(claimed.GetReclaims()) != 1 || claimed.GetReclaims()[0].GetBackendHandle() != created.GetID() {
		t.Fatalf("claimed reclaim = %#v, err=%v", claimed, err)
	}
	reclaim := claimed.GetReclaims()[0]
	if _, err := controller.ReportVolumeReclaim(ctx, reclaimReport(reclaim, false, "node unavailable")); err != nil {
		t.Fatal(err)
	}
	response, err = controller.DeleteWorkloadVolumeClaims(ctx, &privatestoragev1.DeleteWorkloadVolumeClaimsRequest{Namespace: "default", WorkloadID: "svc-1"})
	if err != nil || response.GetComplete() || len(response.GetReclaims()) != 0 {
		t.Fatalf("backoff reclaim = %#v, err=%v", response, err)
	}
	current, ok, err := store.GetVolumeClaim(ctx, created.GetNamespace(), created.GetName())
	if err != nil || !ok {
		t.Fatalf("get failed reclaim ok=%v err=%v", ok, err)
	}
	_, err = store.UpdateVolumeClaim(ctx, current.GetNamespace(), current.GetName(), current.GetVersion(), func(next *storagev1.VolumeClaim) error {
		next.NextReclaimAt = timestamppb.New(time.Now().Add(-time.Second))
		next.Version++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err = controller.ClaimVolumeReclaims(ctx, &privatestoragev1.ClaimVolumeReclaimsRequest{Limit: 1, LeaseOwner: "worker-b"})
	if err != nil || len(claimed.GetReclaims()) != 1 || claimed.GetReclaims()[0].GetAttempt() != 2 {
		t.Fatalf("retried reclaim = %#v, err=%v", claimed, err)
	}
	if _, err := controller.ReportVolumeReclaim(ctx, reclaimReport(claimed.GetReclaims()[0], true, "")); err != nil {
		t.Fatal(err)
	}
	response, err = controller.DeleteWorkloadVolumeClaims(ctx, &privatestoragev1.DeleteWorkloadVolumeClaimsRequest{Namespace: "default", WorkloadID: "svc-1"})
	if err != nil || !response.GetComplete() {
		t.Fatalf("completed reclaim = %#v, err=%v", response, err)
	}
	recreated, err := controller.CreateVolumeClaim(ctx, &storagev1.CreateVolumeClaimRequest{
		Namespace: "default", Name: created.GetName(), ClassName: "local",
		AccessMode:    storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		ReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE,
		BindingScope:  storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
	})
	if err != nil {
		t.Fatal(err)
	}
	if recreated.GetID() == created.GetID() {
		t.Fatalf("recreated claim reused old identity %q", recreated.GetID())
	}
	tombstones, err := controller.ListVolumeClaims(ctx, &storagev1.VolumeClaimListFilter{Statuses: []storagev1.VolumeStatus{storagev1.VolumeStatus_VOLUME_STATUS_DELETED}})
	if err != nil || len(tombstones) != 1 || tombstones[0].GetID() != created.GetID() {
		t.Fatalf("tombstones = %#v, err=%v", tombstones, err)
	}
}

func TestDirectClaimDeletionCreatesLeaseableReclaimTask(t *testing.T) {
	ctx := context.Background()
	store := storetest.NewMemoryStore()
	controller := NewController(store)
	now := time.Unix(200, 0).UTC()
	controller.now = func() time.Time { return now }
	mustCreateLocalClass(t, ctx, controller)
	claim := mustCreateClaim(t, ctx, controller, "default", "direct-delete")
	claim, err := store.UpdateVolumeClaim(ctx, claim.GetNamespace(), claim.GetName(), claim.GetVersion(), func(next *storagev1.VolumeClaim) error {
		next.ReclaimPolicy = storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE
		next.Topology = &storagev1.VolumeTopology{NodeID: "node-a"}
		next.Version++
		next.UpdatedAt = timestamppb.New(now)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := controller.DeleteVolumeClaim(ctx, &storagev1.DeleteVolumeClaimRequest{Namespace: claim.GetNamespace(), Name: claim.GetName()})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETING {
		t.Fatalf("status = %s, want deleting", deleted.GetStatus())
	}
	claimed, err := controller.ClaimVolumeReclaims(ctx, &privatestoragev1.ClaimVolumeReclaimsRequest{Limit: 10, LeaseOwner: "worker-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed.GetReclaims()) != 1 || claimed.GetReclaims()[0].GetClaimID() != claim.GetID() || claimed.GetReclaims()[0].GetLeaseToken() == "" {
		t.Fatalf("claimed reclaims = %#v", claimed.GetReclaims())
	}
	claimedAgain, err := controller.ClaimVolumeReclaims(ctx, &privatestoragev1.ClaimVolumeReclaimsRequest{Limit: 10, LeaseOwner: "worker-b"})
	if err != nil || len(claimedAgain.GetReclaims()) != 0 {
		t.Fatalf("duplicate claim = %#v, err=%v", claimedAgain.GetReclaims(), err)
	}
}

func reclaimReport(reclaim *privatestoragev1.VolumeReclaim, succeeded bool, message string) *privatestoragev1.ReportVolumeReclaimRequest {
	return &privatestoragev1.ReportVolumeReclaimRequest{
		ClaimID: reclaim.GetClaimID(), NodeID: reclaim.GetNodeID(), Succeeded: succeeded, Message: message,
		LeaseToken: reclaim.GetLeaseToken(), LeaseOwner: reclaim.GetLeaseOwner(), LeaseGeneration: reclaim.GetLeaseGeneration(),
	}
}

func TestControllerCreatesClassAndClaim(t *testing.T) {
	ctx := context.Background()
	store := storetest.NewMemoryStore()
	controller := NewController(store)

	class, err := controller.CreateVolumeClass(ctx, &storagev1.CreateVolumeClassRequest{
		Name:                 "local",
		Backend:              storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		AccessModes:          []storagev1.VolumeAccessMode{storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE},
		DefaultReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		ConsistencyProfile:   storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{
			SupportsRunc:  true,
			SupportsRunsc: true,
		},
	})
	if err != nil {
		t.Fatalf("CreateVolumeClass() error = %v", err)
	}
	if class.GetName() != "local" || class.GetCreatedAt() == nil {
		t.Fatalf("class = %#v, want persisted class with timestamps", class)
	}

	claim, err := controller.CreateVolumeClaim(ctx, &storagev1.CreateVolumeClaimRequest{
		Namespace:     "default",
		Name:          "data",
		ClassName:     "local",
		AccessMode:    storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		ReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		BindingScope:  storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
		RequestedCapacity: &storagev1.VolumeCapacity{
			SizeBytes: 10 << 30,
		},
	})
	if err != nil {
		t.Fatalf("CreateVolumeClaim() error = %v", err)
	}
	if !strings.HasPrefix(claim.GetID(), "claim-") || claim.GetBackendHandle() != claim.GetID() || claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_PENDING {
		t.Fatalf("claim = %#v, want pending claim-owned identity", claim)
	}
}

func TestControllerRejectsUnsupportedAccessMode(t *testing.T) {
	ctx := context.Background()
	controller := NewController(storetest.NewMemoryStore())
	_, err := controller.CreateVolumeClass(ctx, &storagev1.CreateVolumeClassRequest{
		Name:                 "local",
		Backend:              storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		AccessModes:          []storagev1.VolumeAccessMode{storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE},
		DefaultReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		ConsistencyProfile:   storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{SupportsRunc: true},
	})
	if err != nil {
		t.Fatalf("CreateVolumeClass() error = %v", err)
	}
	_, err = controller.CreateVolumeClaim(ctx, &storagev1.CreateVolumeClaimRequest{
		Namespace:     "default",
		Name:          "data",
		ClassName:     "local",
		AccessMode:    storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY,
		ReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		BindingScope:  storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
	})
	if err == nil {
		t.Fatal("expected unsupported access mode error")
	}
}

func TestControllerUpdatesAndDeletesClaim(t *testing.T) {
	ctx := context.Background()
	controller := NewController(storetest.NewMemoryStore())
	mustCreateLocalClass(t, ctx, controller)
	claim := mustCreateClaim(t, ctx, controller, "default", "data")

	updated, err := controller.UpdateVolumeClaim(ctx, &storagev1.UpdateVolumeClaimRequest{
		Namespace:       "default",
		Name:            "data",
		ExpectedVersion: claim.GetVersion(),
		RequestedCapacity: &storagev1.VolumeCapacity{
			SizeBytes: 20 << 30,
		},
		Labels: map[string]string{"tier": "hot"},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"requested_capacity", "labels"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateVolumeClaim() error = %v", err)
	}
	if updated.GetVersion() != claim.GetVersion()+1 {
		t.Fatalf("updated version = %d, want %d", updated.GetVersion(), claim.GetVersion()+1)
	}
	if updated.GetRequestedCapacity().GetSizeBytes() != 20<<30 || updated.GetLabels()["tier"] != "hot" {
		t.Fatalf("updated claim = %#v, want capacity and labels update", updated)
	}

	deleted, err := controller.DeleteVolumeClaim(ctx, &storagev1.DeleteVolumeClaimRequest{
		Namespace: "default",
		Name:      "data",
	})
	if err != nil {
		t.Fatalf("DeleteVolumeClaim() error = %v", err)
	}
	if deleted.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		t.Fatalf("deleted status = %s, want deleted", deleted.GetStatus())
	}
	if _, ok, err := controller.GetVolumeClaim(ctx, "default", "data"); err != nil || !ok {
		t.Fatalf("GetVolumeClaim() after delete = ok %v err %v, want retained record", ok, err)
	}
}

func TestControllerDeletesFailedClaim(t *testing.T) {
	ctx := context.Background()
	store := storetest.NewMemoryStore()
	controller := NewController(store)
	mustCreateLocalClass(t, ctx, controller)
	claim := mustCreateClaim(t, ctx, controller, "default", "data")

	_, err := store.UpdateVolumeClaim(ctx, "default", "data", claim.GetVersion(), func(claim *storagev1.VolumeClaim) error {
		claim.Status = storagev1.VolumeStatus_VOLUME_STATUS_FAILED
		claim.Version++
		return nil
	})
	if err != nil {
		t.Fatalf("mark failed claim error = %v", err)
	}

	deleted, err := controller.DeleteVolumeClaim(ctx, &storagev1.DeleteVolumeClaimRequest{
		Namespace: "default",
		Name:      "data",
	})
	if err != nil {
		t.Fatalf("DeleteVolumeClaim(failed) error = %v", err)
	}
	if deleted.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		t.Fatalf("deleted failed claim status = %s, want deleted", deleted.GetStatus())
	}
}

func TestControllerRejectsUnknownUpdateMaskPath(t *testing.T) {
	ctx := context.Background()
	controller := NewController(storetest.NewMemoryStore())
	mustCreateLocalClass(t, ctx, controller)
	mustCreateClaim(t, ctx, controller, "default", "data")

	_, err := controller.UpdateVolumeClaim(ctx, &storagev1.UpdateVolumeClaimRequest{
		Namespace: "default",
		Name:      "data",
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"status"},
		},
	})
	if err == nil {
		t.Fatal("expected unsupported update mask error")
	}
}

func TestControllerListsClaimsByStatusAndLabels(t *testing.T) {
	ctx := context.Background()
	controller := NewController(storetest.NewMemoryStore())
	mustCreateLocalClass(t, ctx, controller)
	mustCreateClaim(t, ctx, controller, "default", "data-a")
	claim := mustCreateClaim(t, ctx, controller, "default", "data-b")
	_, err := controller.UpdateVolumeClaim(ctx, &storagev1.UpdateVolumeClaimRequest{
		Namespace:       "default",
		Name:            "data-b",
		ExpectedVersion: claim.GetVersion(),
		Labels:          map[string]string{"tier": "hot"},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
	})
	if err != nil {
		t.Fatalf("UpdateVolumeClaim() error = %v", err)
	}

	claims, err := controller.ListVolumeClaims(ctx, &storagev1.VolumeClaimListFilter{
		Namespace: "default",
		Statuses:  []storagev1.VolumeStatus{storagev1.VolumeStatus_VOLUME_STATUS_PENDING},
		Labels:    map[string]string{"tier": "hot"},
	})
	if err != nil {
		t.Fatalf("ListVolumeClaims() error = %v", err)
	}
	if len(claims) != 1 || claims[0].GetName() != "data-b" {
		t.Fatalf("claims = %#v, want only data-b", claims)
	}
}

func TestControllerReservesLocalVolumeBindingAndReturnsTopology(t *testing.T) {
	ctx := context.Background()
	controller := NewController(storetest.NewMemoryStore())
	mustCreateLocalClass(t, ctx, controller)
	mustCreateClaim(t, ctx, controller, "default", "data")

	reserve, err := controller.ReserveVolumeBinding(ctx, &privatestoragev1.ReserveVolumeBindingRequest{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{
			ClaimName: "data",
			Target:    "/data",
			Options:   []string{"nodev"},
		}},
	})
	if err != nil {
		t.Fatalf("ReserveVolumeBinding() error = %v", err)
	}
	if len(reserve.GetVolumes()) != 1 {
		t.Fatalf("reserved volumes = %#v, want one", reserve.GetVolumes())
	}
	volume := reserve.GetVolumes()[0]
	if !strings.HasPrefix(volume.GetClaimID(), "claim-") || volume.GetBackendHandle() != volume.GetClaimID() || volume.GetTopology().GetNodeID() != "node-a" || volume.GetTarget() != "/data" {
		t.Fatalf("reserved volume = %#v, want claim-owned volume on node-a at /data", volume)
	}

	requirements, err := controller.ResolveVolumeRequirements(ctx, &privatestoragev1.ResolveVolumeRequirementsRequest{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{
			ClaimName: "data",
			Target:    "/data",
			Options:   []string{"nodev"},
		}},
	})
	if err != nil {
		t.Fatalf("ResolveVolumeRequirements() error = %v", err)
	}
	if len(requirements.GetRequirements()) != 1 || requirements.GetRequirements()[0].GetTopology().GetNodeID() != "node-a" {
		t.Fatalf("requirements = %#v, want node-a topology", requirements.GetRequirements())
	}
	if _, err := controller.ResolveVolumeRequirements(ctx, &privatestoragev1.ResolveVolumeRequirementsRequest{
		Namespace: "default", WorkloadID: "svc-other", WorkloadType: "service",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{ClaimName: "data", Target: "/data"}},
	}); err == nil || !strings.Contains(err.Error(), "owned by another workload") {
		t.Fatalf("ResolveVolumeRequirements(foreign owner) error = %v", err)
	}

	_, err = controller.ReserveVolumeBinding(ctx, &privatestoragev1.ReserveVolumeBindingRequest{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-2",
		NodeID:       "node-b",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{
			ClaimName: "data",
			Target:    "/data",
			Options:   []string{"nodev"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("ReserveVolumeBinding(conflicting node) error = %v, want already bound", err)
	}

	if _, err := controller.ReportVolumePublish(ctx, &privatestoragev1.ReportVolumePublishRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Observations: []*privatestoragev1.VolumePublishObservation{{
			BindingID: volume.GetBindingID(),
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
			PublishedVolume: &privatestoragev1.PublishedNodeVolume{
				ClaimID:   volume.GetClaimID(),
				BindingID: volume.GetBindingID(),
				Backend:   volume.GetBackend(),
				HostPath:  "/var/lib/volumed/local/default/svc-123/data",
				Target:    volume.GetTarget(),
				Options:   append([]string(nil), volume.GetOptions()...),
			},
		}},
	}); err != nil {
		t.Fatalf("ReportVolumePublish() error = %v", err)
	}
	claim, ok, err := controller.GetVolumeClaim(ctx, "default", "data")
	if err != nil || !ok {
		t.Fatalf("GetVolumeClaim() after publish = ok %v err %v", ok, err)
	}
	if claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED {
		t.Fatalf("claim status after publish = %s, want published", claim.GetStatus())
	}
	health, err := controller.GetVolumeBindingHealth(ctx, 0)
	if err != nil {
		t.Fatalf("GetVolumeBindingHealth() error = %v", err)
	}
	if health.GetPublishedBindings() != 1 || health.GetActiveBindings() != 1 || health.GetTotalBindings() != 1 {
		t.Fatalf("binding health after publish = %#v, want one active published binding", health)
	}
	reservedAgain, err := controller.ReserveVolumeBinding(ctx, &privatestoragev1.ReserveVolumeBindingRequest{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{
			ClaimName: "data",
			Target:    "/data",
			Options:   []string{"nodev"},
		}},
	})
	if err != nil {
		t.Fatalf("ReserveVolumeBinding(idempotent published binding) error = %v", err)
	}
	if len(reservedAgain.GetVolumes()) != 1 || reservedAgain.GetVolumes()[0].GetBindingID() != volume.GetBindingID() {
		t.Fatalf("idempotent reserve volumes = %#v, want existing binding", reservedAgain.GetVolumes())
	}
	health, err = controller.GetVolumeBindingHealth(ctx, 0)
	if err != nil {
		t.Fatalf("GetVolumeBindingHealth(after idempotent reserve) error = %v", err)
	}
	if health.GetPublishedBindings() != 1 || health.GetFailedBindings() != 0 {
		t.Fatalf("binding health after idempotent reserve = %#v, want published binding preserved", health)
	}

	if _, err := controller.ReleaseVolumeBinding(ctx, &privatestoragev1.ReleaseVolumeBindingRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
	}); err != nil {
		t.Fatalf("ReleaseVolumeBinding() error = %v", err)
	}
	if _, err := controller.ReleaseVolumeBinding(ctx, &privatestoragev1.ReleaseVolumeBindingRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
	}); err != nil {
		t.Fatalf("ReleaseVolumeBinding() second call error = %v", err)
	}
	if _, err := controller.ReportVolumeRelease(ctx, &privatestoragev1.ReportVolumeReleaseRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Observations: []*privatestoragev1.VolumeReleaseObservation{{
			BindingID: volume.GetBindingID(),
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_DELETED,
		}},
	}); err != nil {
		t.Fatalf("ReportVolumeRelease(idempotent deleted observation) error = %v", err)
	}
	if _, err := controller.ReportVolumeRelease(ctx, &privatestoragev1.ReportVolumeReleaseRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Observations: []*privatestoragev1.VolumeReleaseObservation{{
			BindingID: volume.GetBindingID(),
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
			Message:   "late unpublish failure",
		}},
	}); err == nil || !strings.Contains(err.Error(), "already deleted") {
		t.Fatalf("ReportVolumeRelease(failed after deleted) error = %v, want already deleted", err)
	}
	if _, err := controller.ReportVolumePublish(ctx, &privatestoragev1.ReportVolumePublishRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Observations: []*privatestoragev1.VolumePublishObservation{{
			BindingID: volume.GetBindingID(),
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
			PublishedVolume: &privatestoragev1.PublishedNodeVolume{
				ClaimID:   volume.GetClaimID(),
				BindingID: volume.GetBindingID(),
				Backend:   volume.GetBackend(),
				HostPath:  "/var/lib/volumed/local/default/svc-123/data",
				Target:    volume.GetTarget(),
			},
		}},
	}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ReportVolumePublish(after release) error = %v, want terminal binding rejection", err)
	}
	claim, ok, err = controller.GetVolumeClaim(ctx, "default", "data")
	if err != nil || !ok {
		t.Fatalf("GetVolumeClaim() after binding release = ok %v err %v", ok, err)
	}
	if claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND {
		t.Fatalf("claim status after allocation binding release = %s, want bound", claim.GetStatus())
	}
	health, err = controller.GetVolumeBindingHealth(ctx, 0)
	if err != nil {
		t.Fatalf("GetVolumeBindingHealth(after release) error = %v", err)
	}
	if health.GetDeletedBindings() != 1 || health.GetActiveBindings() != 0 || health.GetTotalBindings() != 1 {
		t.Fatalf("binding health after release = %#v, want one deleted binding and no active bindings", health)
	}
}

func TestControllerRetriesFailedBinding(t *testing.T) {
	ctx := context.Background()
	controller := NewController(storetest.NewMemoryStore())
	mustCreateLocalClass(t, ctx, controller)
	mustCreateClaim(t, ctx, controller, "default", "data")

	reserve, err := controller.ReserveVolumeBinding(ctx, &privatestoragev1.ReserveVolumeBindingRequest{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{
			ClaimName: "data",
			Target:    "/data",
		}},
	})
	if err != nil {
		t.Fatalf("ReserveVolumeBinding() error = %v", err)
	}
	volume := reserve.GetVolumes()[0]
	if _, err := controller.ReportVolumePublish(ctx, &privatestoragev1.ReportVolumePublishRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Observations: []*privatestoragev1.VolumePublishObservation{{
			BindingID: volume.GetBindingID(),
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
			Message:   "publish failed",
		}},
	}); err != nil {
		t.Fatalf("ReportVolumePublish(failed) error = %v", err)
	}
	health, err := controller.GetVolumeBindingHealth(ctx, 0)
	if err != nil {
		t.Fatalf("GetVolumeBindingHealth(after failed publish) error = %v", err)
	}
	if health.GetFailedBindings() != 1 || health.GetActiveBindings() != 1 {
		t.Fatalf("binding health after failed publish = %#v, want one active failed binding", health)
	}
	list, err := controller.ListVolumeBindings(ctx, &privatestoragev1.ListVolumeBindingsRequest{
		Filter: &privatestoragev1.VolumeBindingListFilter{
			Statuses:     []storagev1.VolumeStatus{storagev1.VolumeStatus_VOLUME_STATUS_FAILED},
			Namespace:    "default",
			AllocationID: "alloc-1",
			NodeID:       "node-a",
		},
	})
	if err != nil {
		t.Fatalf("ListVolumeBindings(failed) error = %v", err)
	}
	if len(list.GetBindings()) != 1 || list.GetBindings()[0].GetBindingID() != volume.GetBindingID() {
		t.Fatalf("ListVolumeBindings(failed) = %+v, want failed binding", list.GetBindings())
	}

	retry, err := controller.RetryFailedVolumeBinding(ctx, &privatestoragev1.RetryFailedVolumeBindingRequest{
		BindingID:      volume.GetBindingID(),
		OperatorReason: "node recovered",
	})
	if err != nil {
		t.Fatalf("RetryFailedVolumeBinding() error = %v", err)
	}
	if retry.GetBinding().GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND {
		t.Fatalf("retry binding status = %s, want bound", retry.GetBinding().GetStatus())
	}
	claim, ok, err := controller.GetVolumeClaim(ctx, "default", "data")
	if err != nil || !ok {
		t.Fatalf("GetVolumeClaim() ok=%v err=%v", ok, err)
	}
	if claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND {
		t.Fatalf("claim status after retry = %s, want bound", claim.GetStatus())
	}
	health, err = controller.GetVolumeBindingHealth(ctx, 0)
	if err != nil {
		t.Fatalf("GetVolumeBindingHealth(after retry) error = %v", err)
	}
	if health.GetFailedBindings() != 0 || health.GetActiveBindings() != 1 || health.GetPublishedBindings() != 0 {
		t.Fatalf("binding health after retry = %#v, want one active unpublished binding and no failed bindings", health)
	}
	if _, err := controller.ReportVolumePublish(ctx, &privatestoragev1.ReportVolumePublishRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Observations: []*privatestoragev1.VolumePublishObservation{{
			BindingID: volume.GetBindingID(),
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
			PublishedVolume: &privatestoragev1.PublishedNodeVolume{
				ClaimID:   volume.GetClaimID(),
				BindingID: volume.GetBindingID(),
				Backend:   volume.GetBackend(),
				HostPath:  "/var/lib/volumed/local/default/svc-123/data",
				Target:    volume.GetTarget(),
			},
		}},
	}); err != nil {
		t.Fatalf("ReportVolumePublish(after retry) error = %v", err)
	}
	health, err = controller.GetVolumeBindingHealth(ctx, 0)
	if err != nil {
		t.Fatalf("GetVolumeBindingHealth(after retry publish) error = %v", err)
	}
	if health.GetFailedBindings() != 0 || health.GetPublishedBindings() != 1 {
		t.Fatalf("binding health after retry publish = %#v, want one published binding and no failed bindings", health)
	}
}

func TestControllerRejectsIncompleteReserveAndReleaseRequests(t *testing.T) {
	ctx := context.Background()
	controller := NewController(storetest.NewMemoryStore())
	mustCreateLocalClass(t, ctx, controller)
	mustCreateClaim(t, ctx, controller, "default", "data")

	_, err := controller.ReserveVolumeBinding(ctx, &privatestoragev1.ReserveVolumeBindingRequest{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		NodeID:       "node-a",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{
			ClaimName: "data",
			Target:    "/data",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "allocation id is required") {
		t.Fatalf("ReserveVolumeBinding(empty allocation) error = %v, want allocation id validation", err)
	}

	_, err = controller.ReserveVolumeBinding(ctx, &privatestoragev1.ReserveVolumeBindingRequest{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{
			ClaimName: "data",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "mount target is required") {
		t.Fatalf("ReserveVolumeBinding(empty target) error = %v, want target validation", err)
	}

	_, err = controller.ReleaseVolumeBinding(ctx, &privatestoragev1.ReleaseVolumeBindingRequest{})
	if err == nil || !strings.Contains(err.Error(), "allocation id is required") {
		t.Fatalf("ReleaseVolumeBinding(empty allocation) error = %v, want allocation id validation", err)
	}

	_, err = controller.ReleaseVolumeBinding(ctx, &privatestoragev1.ReleaseVolumeBindingRequest{
		AllocationID: "alloc-1",
	})
	if err == nil || !strings.Contains(err.Error(), "node id is required") {
		t.Fatalf("ReleaseVolumeBinding(empty node) error = %v, want node id validation", err)
	}

	_, err = controller.ReportVolumeRelease(ctx, &privatestoragev1.ReportVolumeReleaseRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Observations: []*privatestoragev1.VolumeReleaseObservation{{
			BindingID: "alloc-1/missing",
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_DELETED,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ReportVolumeRelease(unknown binding) error = %v, want not found", err)
	}
}

func TestControllerRejectsInvalidVolumeObservations(t *testing.T) {
	ctx := context.Background()
	controller := NewController(storetest.NewMemoryStore())
	mustCreateLocalClass(t, ctx, controller)
	mustCreateClaim(t, ctx, controller, "default", "data")
	reserve, err := controller.ReserveVolumeBinding(ctx, &privatestoragev1.ReserveVolumeBindingRequest{
		Namespace:    "default",
		WorkloadID:   "svc-123",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{
			ClaimName: "data",
			Target:    "/data",
		}},
	})
	if err != nil {
		t.Fatalf("ReserveVolumeBinding() error = %v", err)
	}
	bindingID := reserve.GetVolumes()[0].GetBindingID()

	_, err = controller.ReportVolumePublish(ctx, &privatestoragev1.ReportVolumePublishRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Observations: []*privatestoragev1.VolumePublishObservation{{
			BindingID: bindingID,
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "failure message is required") {
		t.Fatalf("ReportVolumePublish(invalid failure) error = %v, want failure message validation", err)
	}

	_, err = controller.ReportVolumeRelease(ctx, &privatestoragev1.ReportVolumeReleaseRequest{
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Observations: []*privatestoragev1.VolumeReleaseObservation{{
			BindingID: bindingID,
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "status VOLUME_STATUS_PUBLISHED is invalid") {
		t.Fatalf("ReportVolumeRelease(invalid status) error = %v, want status validation", err)
	}
}

func mustCreateLocalClass(t *testing.T, ctx context.Context, controller *Controller) {
	t.Helper()
	_, err := controller.CreateVolumeClass(ctx, &storagev1.CreateVolumeClassRequest{
		Name:                 "local",
		Backend:              storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		AccessModes:          []storagev1.VolumeAccessMode{storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE},
		DefaultReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		ConsistencyProfile:   storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{SupportsRunc: true, SupportsRunsc: true},
	})
	if err != nil {
		t.Fatalf("CreateVolumeClass() error = %v", err)
	}
}

func mustCreateClaim(t *testing.T, ctx context.Context, controller *Controller, namespace, name string) *storagev1.VolumeClaim {
	t.Helper()
	claim, err := controller.CreateVolumeClaim(ctx, &storagev1.CreateVolumeClaimRequest{
		Namespace:     namespace,
		Name:          name,
		ClassName:     "local",
		AccessMode:    storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		ReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		BindingScope:  storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
		RequestedCapacity: &storagev1.VolumeCapacity{
			SizeBytes: 10 << 30,
		},
	})
	if err != nil {
		t.Fatalf("CreateVolumeClaim() error = %v", err)
	}
	return claim
}
