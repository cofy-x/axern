package admin

import "testing"

func TestValidateRetryReason(t *testing.T) {
	for _, value := range []string{"create", "delete", " CREATE "} {
		if err := ValidateRetryReason(value); err != nil {
			t.Fatalf("ValidateRetryReason(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"", "start"} {
		if err := ValidateRetryReason(value); err == nil {
			t.Fatalf("ValidateRetryReason(%q) unexpectedly succeeded", value)
		}
	}
}

func TestValidateOwnerType(t *testing.T) {
	for _, value := range []string{"", "run", "service", " SERVICE "} {
		if err := ValidateOwnerType(value); err != nil {
			t.Fatalf("ValidateOwnerType(%q) error = %v", value, err)
		}
	}
	if err := ValidateOwnerType("node"); err == nil {
		t.Fatal("ValidateOwnerType(node) unexpectedly succeeded")
	}
}

func TestValidateOperatorReason(t *testing.T) {
	if err := ValidateOperatorReason("operator checked retry"); err != nil {
		t.Fatalf("ValidateOperatorReason(valid) error = %v", err)
	}
	if err := ValidateOperatorReason(" "); err == nil {
		t.Fatal("ValidateOperatorReason(blank) unexpectedly succeeded")
	}
}

func TestValidateVolumeStatus(t *testing.T) {
	for _, value := range []string{"pending", "bound", "published", "releasing", "failed", "deleted", " FAILED "} {
		if err := ValidateVolumeStatus(value); err != nil {
			t.Fatalf("ValidateVolumeStatus(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"", "mounted"} {
		if err := ValidateVolumeStatus(value); err == nil {
			t.Fatalf("ValidateVolumeStatus(%q) unexpectedly succeeded", value)
		}
	}
}

func TestValidateAuditOperation(t *testing.T) {
	for _, value := range []string{"", AuditOperationForceAllocationLifecycleRetry, AuditOperationFailAllocationLifecycleRetry, AuditOperationRetryStorageBinding, " CLEAR-ALLOCATION-LIFECYCLE-RETRY "} {
		if err := ValidateAuditOperation(value); err != nil {
			t.Fatalf("ValidateAuditOperation(%q) error = %v", value, err)
		}
	}
	if err := ValidateAuditOperation("force-retry"); err == nil {
		t.Fatal("ValidateAuditOperation(force-retry) unexpectedly succeeded")
	}
}

func TestValidateAuditTargetType(t *testing.T) {
	for _, value := range []string{"", AuditTargetTypeAllocation, AuditTargetTypeStorageBinding, " ALLOCATION "} {
		if err := ValidateAuditTargetType(value); err != nil {
			t.Fatalf("ValidateAuditTargetType(%q) error = %v", value, err)
		}
	}
	if err := ValidateAuditTargetType("run"); err == nil {
		t.Fatal("ValidateAuditTargetType(run) unexpectedly succeeded")
	}
}

func TestValidateAuditTargetFilter(t *testing.T) {
	for _, tc := range []struct {
		name       string
		targetType string
		targetID   string
		wantErr    bool
	}{
		{name: "empty"},
		{name: "type only", targetType: "allocation"},
		{name: "type and id", targetType: "allocation", targetID: "alloc-a"},
		{name: "id without type", targetID: "alloc-a", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAuditTargetFilter(tc.targetType, tc.targetID)
			if tc.wantErr && err == nil {
				t.Fatal("ValidateAuditTargetFilter() unexpectedly succeeded")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateAuditTargetFilter() error = %v", err)
			}
		})
	}
}
