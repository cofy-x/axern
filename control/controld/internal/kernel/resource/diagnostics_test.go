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

func TestMessageIndicatesAdmissionBlocked(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "quota", message: "namespace quota exceeded: namespace=team-a", want: true},
		{name: "reservation", message: "no node has remaining reservation capacity: node_id=node-a", want: true},
		{name: "placement capacity", message: "no eligible node: rejection_reasons=insufficient_cpu", want: true},
		{name: "runtime resource exhausted", message: "allocate resource interface failed: resource exhausted", want: false},
		{name: "selection", message: "no eligible node: rejection_reasons=runtime_unsupported", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MessageIndicatesAdmissionBlocked(tt.message); got != tt.want {
				t.Fatalf("MessageIndicatesAdmissionBlocked(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}
