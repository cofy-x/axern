package storage

import (
	"testing"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
)

func TestValidateVolumeClassCreate(t *testing.T) {
	err := ValidateVolumeClassCreate(VolumeClassCreate{
		Name:                 "local",
		Backend:              storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		AccessModes:          []storagev1.VolumeAccessMode{storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE},
		DefaultReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		ConsistencyProfile:   storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{SupportsRunsc: true},
	})
	if err != nil {
		t.Fatalf("ValidateVolumeClassCreate() error = %v", err)
	}
}

func TestStableNameRejectsPathLikeValues(t *testing.T) {
	for _, value := range []string{"", "bad/name", "../data", "has space"} {
		if StableName(value) {
			t.Fatalf("StableName(%q) = true, want false", value)
		}
	}
}

func TestValidateVolumeClaimOwnership(t *testing.T) {
	claim := &storagev1.VolumeClaim{
		Namespace: "default", Name: "workspace",
		BindingScope: storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
		OwnerID:      "svc-1", OwnerType: "service",
	}
	if err := ValidateVolumeClaimOwnership(claim, "svc-1", "service"); err != nil {
		t.Fatalf("matching owner error = %v", err)
	}
	for _, owner := range [][2]string{{"svc-2", "service"}, {"svc-1", "run"}, {"", ""}} {
		if err := ValidateVolumeClaimOwnership(claim, owner[0], owner[1]); err == nil {
			t.Fatalf("owner %q/%q accepted, want rejection", owner[1], owner[0])
		}
	}
	claim.OwnerID = ""
	claim.OwnerType = ""
	if err := ValidateVolumeClaimOwnership(claim, "", ""); err == nil {
		t.Fatal("empty claim and request ownership accepted, want workload identity rejection")
	}
	claim.BindingScope = storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_ALLOCATION
	if err := ValidateVolumeClaimOwnership(claim, "", ""); err != nil {
		t.Fatalf("allocation-scoped owner validation error = %v", err)
	}
}

func TestTransitionClaimStatus(t *testing.T) {
	allowed := []struct {
		name    string
		current storagev1.VolumeStatus
		next    storagev1.VolumeStatus
	}{
		{"pending to bound after reservation", storagev1.VolumeStatus_VOLUME_STATUS_PENDING, storagev1.VolumeStatus_VOLUME_STATUS_BOUND},
		{"pending to releasing on delete before binding", storagev1.VolumeStatus_VOLUME_STATUS_PENDING, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING},
		{"pending to failed on admission failure", storagev1.VolumeStatus_VOLUME_STATUS_PENDING, storagev1.VolumeStatus_VOLUME_STATUS_FAILED},
		{"bound to publishing", storagev1.VolumeStatus_VOLUME_STATUS_BOUND, storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHING},
		{"bound to published after synchronous node publish", storagev1.VolumeStatus_VOLUME_STATUS_BOUND, storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED},
		{"bound to releasing on claim delete", storagev1.VolumeStatus_VOLUME_STATUS_BOUND, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING},
		{"bound to failed on publish failure", storagev1.VolumeStatus_VOLUME_STATUS_BOUND, storagev1.VolumeStatus_VOLUME_STATUS_FAILED},
		{"publishing to published", storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHING, storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED},
		{"publishing to releasing", storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHING, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING},
		{"publishing to failed", storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHING, storagev1.VolumeStatus_VOLUME_STATUS_FAILED},
		{"published to bound after allocation release", storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED, storagev1.VolumeStatus_VOLUME_STATUS_BOUND},
		{"published to releasing on claim delete", storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING},
		{"published to failed on node observation", storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED, storagev1.VolumeStatus_VOLUME_STATUS_FAILED},
		{"releasing to deleted", storagev1.VolumeStatus_VOLUME_STATUS_RELEASING, storagev1.VolumeStatus_VOLUME_STATUS_DELETED},
		{"releasing to failed when cleanup fails", storagev1.VolumeStatus_VOLUME_STATUS_RELEASING, storagev1.VolumeStatus_VOLUME_STATUS_FAILED},
		{"failed to bound on retry", storagev1.VolumeStatus_VOLUME_STATUS_FAILED, storagev1.VolumeStatus_VOLUME_STATUS_BOUND},
		{"failed to releasing", storagev1.VolumeStatus_VOLUME_STATUS_FAILED, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING},
	}
	for _, tc := range allowed {
		t.Run(tc.name, func(t *testing.T) {
			if err := TransitionClaimStatus(tc.current, tc.next); err != nil {
				t.Fatalf("TransitionClaimStatus(%s,%s) error = %v", tc.current, tc.next, err)
			}
		})
	}

	rejected := []struct {
		name    string
		current storagev1.VolumeStatus
		next    storagev1.VolumeStatus
	}{
		{"deleted is terminal", storagev1.VolumeStatus_VOLUME_STATUS_DELETED, storagev1.VolumeStatus_VOLUME_STATUS_BOUND},
		{"pending cannot publish without binding", storagev1.VolumeStatus_VOLUME_STATUS_PENDING, storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED},
		{"releasing cannot become bound without retry via failed", storagev1.VolumeStatus_VOLUME_STATUS_RELEASING, storagev1.VolumeStatus_VOLUME_STATUS_BOUND},
		{"failed cannot be published without reservation retry", storagev1.VolumeStatus_VOLUME_STATUS_FAILED, storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if err := TransitionClaimStatus(tc.current, tc.next); err == nil {
				t.Fatalf("TransitionClaimStatus(%s,%s) error = nil, want rejection", tc.current, tc.next)
			}
		})
	}
}
