package adminkernel

import "testing"

func TestNormalizeAuditEventFilter(t *testing.T) {
	got := NormalizeAuditEventFilter(AuditEventFilter{Limit: 0})
	if got.Limit != DefaultAuditEventListLimit {
		t.Fatalf("default limit = %d, want %d", got.Limit, DefaultAuditEventListLimit)
	}
	got = NormalizeAuditEventFilter(AuditEventFilter{Limit: MaxAuditEventListLimit + 1})
	if got.Limit != MaxAuditEventListLimit {
		t.Fatalf("capped limit = %d, want %d", got.Limit, MaxAuditEventListLimit)
	}
}

func TestValidateAuditEventFilter(t *testing.T) {
	valid := AuditEventFilter{
		Operation:  AuditOperationForceAllocationLifecycleRetry,
		TargetType: AuditTargetAllocation,
		TargetID:   "alloc-a",
	}
	if err := ValidateAuditEventFilter(valid); err != nil {
		t.Fatalf("ValidateAuditEventFilter(valid) error = %v", err)
	}
	for _, filter := range []AuditEventFilter{
		{Operation: "force_retry"},
		{TargetType: "run"},
		{TargetID: "alloc-a"},
	} {
		if err := ValidateAuditEventFilter(filter); err == nil {
			t.Fatalf("ValidateAuditEventFilter(%+v) unexpectedly succeeded", filter)
		}
	}
}
