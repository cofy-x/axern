package storage

import (
	"context"
	"testing"
	"time"

	storagekernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	"github.com/cofy-x/axern/control/storaged/internal/storetest"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestReleaseWorkloadVolumeClaimsAllowsReattachWithSameBackend(t *testing.T) {
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
		claim.BackendHandle = created.GetID()
		claim.Version++
		claim.UpdatedAt = timestamppb.New(now)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	released, err := controller.ReleaseWorkloadVolumeClaims(ctx, &privatestoragev1.ReleaseWorkloadVolumeClaimsRequest{
		Namespace: "default", WorkloadID: "svc-1", WorkloadType: "service",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(released.GetReleasedClaimIds()) != 1 || released.GetReleasedClaimIds()[0] != created.GetID() {
		t.Fatalf("released = %#v", released)
	}
	current, ok, err := store.GetVolumeClaim(ctx, created.GetNamespace(), created.GetName())
	if err != nil || !ok {
		t.Fatalf("get released claim ok=%v err=%v", ok, err)
	}
	if current.GetOwnerID() != "" || current.GetOwnerType() != "" {
		t.Fatalf("released claim owner = %q/%q, want empty", current.GetOwnerID(), current.GetOwnerType())
	}
	if current.GetBackendHandle() != created.GetID() || current.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_PENDING {
		t.Fatalf("released claim backend=%q status=%v, want original backend and live status", current.GetBackendHandle(), current.GetStatus())
	}
	again, err := controller.ReleaseWorkloadVolumeClaims(ctx, &privatestoragev1.ReleaseWorkloadVolumeClaimsRequest{
		Namespace: "default", WorkloadID: "svc-1", WorkloadType: "service",
	})
	if err != nil || len(again.GetReleasedClaimIds()) != 0 {
		t.Fatalf("second release = %#v err=%v, want idempotent no-op", again, err)
	}
	if _, err := controller.ResolveVolumeRequirements(ctx, &privatestoragev1.ResolveVolumeRequirementsRequest{
		Namespace: "default", WorkloadID: "svc-2", WorkloadType: "service",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{ClaimName: created.GetName(), Target: "/home/axern/workspace"}},
	}); err != nil {
		t.Fatalf("new workload resolve after release: %v", err)
	}
	reattached, ok, err := store.GetVolumeClaim(ctx, created.GetNamespace(), created.GetName())
	if err != nil || !ok {
		t.Fatalf("get reattached claim ok=%v err=%v", ok, err)
	}
	if reattached.GetOwnerID() != "svc-2" || reattached.GetBackendHandle() != created.GetID() {
		t.Fatalf("reattached claim owner=%q backend=%q, want svc-2 with original backend", reattached.GetOwnerID(), reattached.GetBackendHandle())
	}
}

func TestReleaseWorkloadVolumeClaimsIgnoresOtherOwners(t *testing.T) {
	ctx := context.Background()
	store := storetest.NewMemoryStore()
	controller := NewController(store)
	mustCreateLocalClass(t, ctx, controller)
	for _, name := range []string{"claim-a", "claim-b"} {
		claim, err := controller.CreateVolumeClaim(ctx, &storagev1.CreateVolumeClaimRequest{
			Namespace: "default", Name: name, ClassName: "local",
			AccessMode:    storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
			ReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE,
			BindingScope:  storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
		})
		if err != nil {
			t.Fatal(err)
		}
		owner := "svc-1"
		if name == "claim-b" {
			owner = "svc-2"
		}
		if _, err := store.UpdateVolumeClaim(ctx, claim.GetNamespace(), claim.GetName(), claim.GetVersion(), func(next *storagev1.VolumeClaim) error {
			next.OwnerType = "service"
			next.OwnerID = owner
			next.Version++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	released, err := controller.ReleaseWorkloadVolumeClaims(ctx, &privatestoragev1.ReleaseWorkloadVolumeClaimsRequest{
		Namespace: "default", WorkloadID: "svc-1", WorkloadType: "service",
	})
	if err != nil || len(released.GetReleasedClaimIds()) != 1 {
		t.Fatalf("released = %#v err=%v, want exactly one claim", released, err)
	}
	other, ok, err := store.GetVolumeClaim(ctx, "default", "claim-b")
	if err != nil || !ok || other.GetOwnerID() != "svc-2" {
		t.Fatalf("other claim owner=%q ok=%v err=%v, want untouched svc-2", other.GetOwnerID(), ok, err)
	}
}

func TestReleaseWorkloadVolumeClaimsRequiresNamespaceAndWorkload(t *testing.T) {
	controller := NewController(storetest.NewMemoryStore())
	if _, err := controller.ReleaseWorkloadVolumeClaims(context.Background(), &privatestoragev1.ReleaseWorkloadVolumeClaimsRequest{}); err == nil {
		t.Fatal("ReleaseWorkloadVolumeClaims() error = nil, want validation failure")
	}
}

func TestReleaseWorkloadVolumeClaimsRejectsActiveBindingWithoutPartialRelease(t *testing.T) {
	ctx := context.Background()
	store := storetest.NewMemoryStore()
	controller := NewController(store)
	mustCreateLocalClass(t, ctx, controller)
	for _, name := range []string{"claim-a", "claim-b"} {
		claim, err := controller.CreateVolumeClaim(ctx, &storagev1.CreateVolumeClaimRequest{
			Namespace: "default", Name: name, ClassName: "local",
			AccessMode:    storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
			ReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
			BindingScope:  storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpdateVolumeClaim(ctx, claim.GetNamespace(), claim.GetName(), claim.GetVersion(), func(next *storagev1.VolumeClaim) error {
			next.OwnerID = "svc-1"
			next.OwnerType = "service"
			next.BackendHandle = next.GetID()
			next.Version++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := controller.ReserveVolumeBinding(ctx, &privatestoragev1.ReserveVolumeBindingRequest{
		Namespace: "default", WorkloadID: "svc-1", WorkloadType: "service", AllocationID: "alloc-1", NodeID: "node-a",
		Mounts: []*privatestoragev1.WorkloadVolumeMount{{ClaimName: "claim-b", Target: "/data"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.ReleaseWorkloadVolumeClaims(ctx, &privatestoragev1.ReleaseWorkloadVolumeClaimsRequest{
		Namespace: "default", WorkloadID: "svc-1", WorkloadType: "service",
	}); err == nil {
		t.Fatal("ReleaseWorkloadVolumeClaims() error = nil, want active binding rejection")
	}
	for _, name := range []string{"claim-a", "claim-b"} {
		claim, ok, err := store.GetVolumeClaim(ctx, "default", name)
		if err != nil || !ok || claim.GetOwnerID() != "svc-1" {
			t.Fatalf("claim %s owner=%q ok=%v err=%v, want atomic svc-1 ownership", name, claim.GetOwnerID(), ok, err)
		}
	}
}

func TestReserveVolumeBindingRejectsStaleOwnerSnapshotAfterRelease(t *testing.T) {
	ctx := context.Background()
	store := storetest.NewMemoryStore()
	controller := NewController(store)
	mustCreateLocalClass(t, ctx, controller)
	claim, err := controller.CreateVolumeClaim(ctx, &storagev1.CreateVolumeClaimRequest{
		Namespace: "default", Name: "workspace", ClassName: "local",
		AccessMode:    storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		ReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		BindingScope:  storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
	})
	if err != nil {
		t.Fatal(err)
	}
	staleClaim, err := store.UpdateVolumeClaim(ctx, claim.GetNamespace(), claim.GetName(), claim.GetVersion(), func(next *storagev1.VolumeClaim) error {
		next.OwnerID = "svc-1"
		next.OwnerType = "service"
		next.BackendHandle = next.GetID()
		next.Version++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	class, ok, err := store.GetVolumeClass(ctx, "local")
	if err != nil || !ok {
		t.Fatalf("GetVolumeClass() ok=%v err=%v", ok, err)
	}
	if _, err := store.ReleaseWorkloadVolumeClaims(ctx, "default", "svc-1", "service", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveVolumeBinding(ctx, storagekernel.VolumeBindingReserve{
		Namespace: "default", WorkloadID: "svc-1", WorkloadType: "service", AllocationID: "alloc-stale", NodeID: "node-a",
		Mount: &privatestoragev1.WorkloadVolumeMount{ClaimName: "workspace", Target: "/data"}, Claim: staleClaim, Class: class,
	}); err == nil {
		t.Fatal("ReserveVolumeBinding() error = nil, want stale owner rejection")
	}
}
