package storagecoord

import "testing"

func TestSanitizeStorageReclaimErrorRedactsSensitiveDiagnostics(t *testing.T) {
	if got := sanitizeStorageReclaimError("delete failed: token=secret-value"); got != "[redacted]" {
		t.Fatalf("sanitized error = %q, want redacted", got)
	}
	if got := sanitizeStorageReclaimError("node unavailable"); got != "node unavailable" {
		t.Fatalf("sanitized safe error = %q", got)
	}
}
