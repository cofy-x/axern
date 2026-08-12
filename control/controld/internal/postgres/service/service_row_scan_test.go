package pgservice

import (
	"fmt"
	"testing"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type cannedServiceRow struct {
	deletionStatus []byte
}

func (r cannedServiceRow) Scan(dest ...any) error {
	if len(dest) != 21 {
		return fmt.Errorf("unexpected scan destination count: %d", len(dest))
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	values := []any{
		"svc-1", "default", "env-1", int32(0), int32(0), int32(0),
		[]byte("{}"), []byte("null"), []byte("null"), []byte("null"), []byte("null"),
		"SERVICE_STATUS_DELETING", []byte("{}"), []byte("[]"), []byte("{}"),
		int64(3), now, now, "releasing", "WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED",
		r.deletionStatus,
	}
	for index, value := range values {
		switch target := dest[index].(type) {
		case *string:
			*target = value.(string)
		case *int32:
			*target = value.(int32)
		case *int64:
			*target = value.(int64)
		case *time.Time:
			*target = value.(time.Time)
		case *[]byte:
			if value == nil {
				*target = nil
			} else {
				*target = value.([]byte)
			}
		default:
			return fmt.Errorf("unsupported scan destination %d: %T", index, dest[index])
		}
	}
	return nil
}

func TestScanServiceRestoresDeletionStatus(t *testing.T) {
	payload, err := protojson.Marshal(&servicev1.ServiceDeletionStatus{
		Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES,
		VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE,
		ClaimIds:          []string{"claim-1", "claim-2"},
		Message:           "reclaiming service volumes",
	})
	if err != nil {
		t.Fatalf("marshal deletion status: %v", err)
	}
	service, err := scanService(cannedServiceRow{deletionStatus: payload})
	if err != nil {
		t.Fatalf("scanService() error = %v", err)
	}
	deletion := service.GetDeletionStatus()
	if deletion == nil {
		t.Fatal("scanService() deletion status = nil, want restored value")
	}
	if deletion.GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES ||
		deletion.GetVolumeDisposition() != servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE ||
		len(deletion.GetClaimIds()) != 2 || deletion.GetClaimIds()[0] != "claim-1" ||
		deletion.GetMessage() != "reclaiming service volumes" {
		t.Fatalf("scanService() deletion status = %#v", deletion)
	}
}

func TestScanServiceTreatsNullDeletionStatusAsAbsent(t *testing.T) {
	for name, payload := range map[string][]byte{
		"sql null":    nil,
		"json null":   []byte("null"),
		"empty value": {},
	} {
		t.Run(name, func(t *testing.T) {
			service, err := scanService(cannedServiceRow{deletionStatus: payload})
			if err != nil {
				t.Fatalf("scanService() error = %v", err)
			}
			if service.GetDeletionStatus() != nil {
				t.Fatalf("scanService() deletion status = %#v, want nil", service.GetDeletionStatus())
			}
		})
	}
}
