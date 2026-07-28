package storage

import (
	"strings"
	"testing"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBuildVolumeBindingHealth(t *testing.T) {
	health := BuildVolumeBindingHealth(
		map[storagev1.VolumeStatus]int64{
			storagev1.VolumeStatus_VOLUME_STATUS_BOUND:       2,
			storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:   3,
			storagev1.VolumeStatus_VOLUME_STATUS_RELEASING:   4,
			storagev1.VolumeStatus_VOLUME_STATUS_FAILED:      5,
			storagev1.VolumeStatus_VOLUME_STATUS_DELETED:     6,
			storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED: 7,
		},
		map[storagev1.VolumeStatus]int64{
			storagev1.VolumeStatus_VOLUME_STATUS_BOUND: 1,
		},
		2,
		HealthConsistencyCounts{InconsistentClaims: 8, InvalidBindings: 9},
	)
	if health.GetTotalBindings() != 27 {
		t.Fatalf("total bindings = %d, want 27", health.GetTotalBindings())
	}
	if health.GetActiveBindings() != 14 {
		t.Fatalf("active bindings = %d, want 14", health.GetActiveBindings())
	}
	if health.GetPublishedBindings() != 3 || health.GetReleasingBindings() != 4 || health.GetFailedBindings() != 5 || health.GetDeletedBindings() != 6 {
		t.Fatalf("categorized health = %#v", health)
	}
	if health.GetStuckReleasingBindings() != 2 {
		t.Fatalf("stuck releasing bindings = %d, want 2", health.GetStuckReleasingBindings())
	}
	if health.GetInconsistentClaims() != 8 || health.GetInvalidBindings() != 9 {
		t.Fatalf("consistency counts = inconsistent:%d invalid:%d, want 8/9", health.GetInconsistentClaims(), health.GetInvalidBindings())
	}
	if got := health.GetBindingStatusCounts(); len(got) != 6 || got[0].GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED || got[5].GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_RELEASING {
		t.Fatalf("binding status counts order = %#v", got)
	}
}

func TestEvaluateHealthConsistency(t *testing.T) {
	counts := EvaluateHealthConsistency(
		[]ClaimHealthState{
			{ClaimID: "claim-published", Status: storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED},
			{ClaimID: "claim-mismatch", Status: storagev1.VolumeStatus_VOLUME_STATUS_BOUND},
		},
		[]*privatestoragev1.VolumeBinding{{
			ClaimID: "claim-published",
			Status:  storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
			PublishedVolume: &privatestoragev1.PublishedNodeVolume{
				BindingID: "bind-published",
			},
			PublishedAt: timestamppb.Now(),
		}, {
			ClaimID: "claim-mismatch",
			Status:  storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
			PublishedVolume: &privatestoragev1.PublishedNodeVolume{
				BindingID: "bind-mismatch",
			},
			PublishedAt: timestamppb.Now(),
		}, {
			ClaimID: "claim-invalid",
			Status:  storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
		}, {
			ClaimID: "claim-deleted-invalid",
			Status:  storagev1.VolumeStatus_VOLUME_STATUS_DELETED,
		}},
	)
	if counts.InconsistentClaims != 1 || counts.InvalidBindings != 2 {
		t.Fatalf("consistency counts = %+v, want one inconsistent claim and two invalid bindings", counts)
	}
}

func TestValidateVolumePublishObservation(t *testing.T) {
	tests := []struct {
		name        string
		observation *privatestoragev1.VolumePublishObservation
		want        string
	}{
		{
			name:        "unspecified status",
			observation: &privatestoragev1.VolumePublishObservation{BindingID: "binding-1"},
			want:        "status VOLUME_STATUS_UNSPECIFIED is invalid",
		},
		{
			name: "published requires payload",
			observation: &privatestoragev1.VolumePublishObservation{
				BindingID: "binding-1",
				Status:    storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
			},
			want: "published volume is required",
		},
		{
			name: "failed requires message",
			observation: &privatestoragev1.VolumePublishObservation{
				BindingID: "binding-1",
				Status:    storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
			},
			want: "failure message is required",
		},
		{
			name: "valid failure",
			observation: &privatestoragev1.VolumePublishObservation{
				BindingID: "binding-1",
				Status:    storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
				Message:   "volume publish failed: provider validation",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVolumePublishObservation(tt.observation)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ValidateVolumePublishObservation() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateVolumePublishObservation() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateVolumeReleaseObservation(t *testing.T) {
	tests := []struct {
		name        string
		observation *privatestoragev1.VolumeReleaseObservation
		want        string
	}{
		{
			name:        "missing binding",
			observation: &privatestoragev1.VolumeReleaseObservation{Status: storagev1.VolumeStatus_VOLUME_STATUS_DELETED},
			want:        "binding id is required",
		},
		{
			name: "invalid status",
			observation: &privatestoragev1.VolumeReleaseObservation{
				BindingID: "binding-1",
				Status:    storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
			},
			want: "status VOLUME_STATUS_PUBLISHED is invalid",
		},
		{
			name: "failed requires message",
			observation: &privatestoragev1.VolumeReleaseObservation{
				BindingID: "binding-1",
				Status:    storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
			},
			want: "failure message is required",
		},
		{
			name: "valid deleted",
			observation: &privatestoragev1.VolumeReleaseObservation{
				BindingID: "binding-1",
				Status:    storagev1.VolumeStatus_VOLUME_STATUS_DELETED,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVolumeReleaseObservation(tt.observation)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ValidateVolumeReleaseObservation() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateVolumeReleaseObservation() error = %v, want %q", err, tt.want)
			}
		})
	}
}
