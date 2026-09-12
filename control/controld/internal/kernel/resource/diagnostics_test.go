package resource

import "testing"

func TestAdmissionDiagnosticForReason(t *testing.T) {
	tests := []struct {
		reason AdmissionRejectionReason
		want   AdmissionDiagnosticCode
	}{
		{AdmissionRejectionNamespaceQuotaExceeded, AdmissionDiagnosticNamespaceQuotaExceeded},
		{AdmissionRejectionNodeReservationCapacity, AdmissionDiagnosticNodeReservationCapacity},
		{AdmissionRejectionPlacementCapacity, AdmissionDiagnosticPlacementCapacity},
		{AdmissionRejectionNodeSelection, AdmissionDiagnosticNodeSelection},
		{"unknown", AdmissionDiagnosticUnspecified},
	}
	for _, tt := range tests {
		if got := AdmissionDiagnosticForReason(tt.reason); got != tt.want {
			t.Fatalf("AdmissionDiagnosticForReason(%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}
}
