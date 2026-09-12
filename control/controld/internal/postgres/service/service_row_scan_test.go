package pgservice

import (
	"fmt"
	"testing"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type cannedServiceRow struct {
	config         []byte
	deletionStatus []byte
}

func (r cannedServiceRow) Scan(dest ...any) error {
	if len(dest) != 21 {
		return fmt.Errorf("unexpected scan destination count: %d", len(dest))
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	config := r.config
	if len(config) == 0 {
		config = []byte("{}")
	}
	values := []any{
		"svc-1", "default", "env-1", int32(0), int32(0), int32(0),
		[]byte("{}"), []byte("null"), []byte("null"), []byte("null"), []byte("null"),
		"SERVICE_STATUS_DELETING", config, []byte("[]"), []byte("{}"),
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
		Phase:   servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS,
		Message: "releasing service allocations",
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
	if deletion.GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS ||
		deletion.GetMessage() != "releasing service allocations" {
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

func TestScanServiceRejectsRetiredVolumePayloads(t *testing.T) {
	for name, row := range map[string]cannedServiceRow{
		"volume config":      {config: []byte(`{"volumeMounts":[{"name":"data","target":"/data"}]}`)},
		"volume disposition": {deletionStatus: []byte(`{"volumeDisposition":"SERVICE_VOLUME_DISPOSITION_RETAIN"}`)},
		"claim identity":     {deletionStatus: []byte(`{"claimIds":["retained-claim"]}`)},
		"reclaim phase":      {deletionStatus: []byte(`{"phase":"SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			if service, err := scanService(row); err == nil || service != nil {
				t.Fatalf("retired storage payload accepted: service=%v error=%v", service, err)
			}
		})
	}
}
