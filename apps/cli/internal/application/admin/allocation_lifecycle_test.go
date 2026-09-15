package admin

import "testing"

func TestValidateOperatorReason(t *testing.T) {
	if err := ValidateOperatorReason("operator checked retry"); err != nil {
		t.Fatalf("ValidateOperatorReason(valid) error = %v", err)
	}
	if err := ValidateOperatorReason(" "); err == nil {
		t.Fatal("ValidateOperatorReason(blank) unexpectedly succeeded")
	}
}

func TestValidateAuditOperation(t *testing.T) {
	for _, value := range []string{"", AuditOperationForceAllocationLifecycleRetry, AuditOperationFailAllocationLifecycleRetry, " CLEAR-ALLOCATION-LIFECYCLE-RETRY "} {
		if err := ValidateAuditOperation(value); err != nil {
			t.Fatalf("ValidateAuditOperation(%q) error = %v", value, err)
		}
	}
	if err := ValidateAuditOperation("force-retry"); err == nil {
		t.Fatal("ValidateAuditOperation(force-retry) unexpectedly succeeded")
	}
}

func TestValidateAuditTargetType(t *testing.T) {
	for _, value := range []string{"", AuditTargetTypeAllocation, " ALLOCATION "} {
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
