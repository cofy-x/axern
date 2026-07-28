package parse

import (
	"reflect"
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestServiceVolumeMounts(t *testing.T) {
	got, err := ServiceVolumeMounts([]string{"data:/var/lib/app:ro,rbind,nodev"})
	if err != nil {
		t.Fatalf("ServiceVolumeMounts returned error: %v", err)
	}
	want := []*commonv1.ServiceVolumeMount{{
		Name:     "data",
		Target:   "/var/lib/app",
		Readonly: true,
		Options:  []string{"rbind", "nodev"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if _, err := ServiceVolumeMounts([]string{"bad"}); err == nil {
		t.Fatal("expected invalid volume error")
	}
	if _, err := ServiceVolumeMounts([]string{"data:/x:ro,rw"}); err == nil {
		t.Fatal("expected conflicting mode error")
	}
}

func TestImageMounts(t *testing.T) {
	got, err := ImageMounts([]string{"localhost:5000/tools/codex:latest:/opt/axern/tools/codex:ro"})
	if err != nil {
		t.Fatalf("ImageMounts returned error: %v", err)
	}
	want := []*commonv1.ImageMount{{
		Image:    "localhost:5000/tools/codex:latest",
		Target:   "/opt/axern/tools/codex",
		Readonly: true,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if _, err := ImageMounts([]string{"bad"}); err == nil {
		t.Fatal("expected invalid image mount error")
	}
	if _, err := ImageMounts([]string{"image:/opt/tool:rw"}); err == nil {
		t.Fatal("expected unsupported writable image mount error")
	}
}

func TestServiceReplicaView(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    servicev1.ServiceReplicaView
		wantErr bool
	}{
		{name: "all", input: "all", want: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_ALL},
		{name: "ended", input: "ended", want: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_ENDED},
		{name: "outdated", input: "outdated", want: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_OUTDATED},
		{name: "updated", input: "updated", want: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UPDATED},
		{name: "invalid", input: "wat", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ServiceReplicaView(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.input)
				}
				if !strings.Contains(err.Error(), "all, current, ended") {
					t.Fatalf("error %q does not include valid values", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestServiceStatuses(t *testing.T) {
	got, err := ServiceStatuses([]string{"ready,degraded", "deleted"})
	if err != nil {
		t.Fatalf("ServiceStatuses returned error: %v", err)
	}
	want := []servicev1.ServiceStatus{
		servicev1.ServiceStatus_SERVICE_STATUS_READY,
		servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		servicev1.ServiceStatus_SERVICE_STATUS_DELETED,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d statuses, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestServiceStatusesRejectsUnknownValue(t *testing.T) {
	if _, err := ServiceStatuses([]string{"wat"}); err == nil {
		t.Fatal("expected error for invalid service status")
	} else if !strings.Contains(err.Error(), "reconciling, ready") {
		t.Fatalf("error %q does not include valid values", err)
	}
}
