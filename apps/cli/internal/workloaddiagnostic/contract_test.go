package workloaddiagnostic

import "testing"

func TestAdmissionDiagnosticContract(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		wantCode    string
		wantSummary string
	}{
		{
			name:        "namespace quota",
			message:     "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a cpu requested_milli=500 reserved_milli=0 limit_milli=100 available_milli=100",
			wantCode:    DiagnosticAdmissionBlocked,
			wantSummary: "namespace quota exceeded",
		},
		{
			name:        "node CPU capacity",
			message:     "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=insufficient_cpu",
			wantCode:    DiagnosticAdmissionBlocked,
			wantSummary: "node CPU capacity exhausted",
		},
		{
			name:        "node memory capacity",
			message:     "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=insufficient_memory",
			wantCode:    DiagnosticAdmissionBlocked,
			wantSummary: "node memory capacity exhausted",
		},
		{
			name:        "node CPU and memory capacity",
			message:     "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=insufficient_cpu,insufficient_memory",
			wantCode:    DiagnosticAdmissionBlocked,
			wantSummary: "node CPU and memory capacity exhausted",
		},
		{
			name:        "transaction reservation capacity",
			message:     "no node has remaining reservation capacity: node_id=node-a effective_allocatable_milli=2000 available_milli=0",
			wantCode:    DiagnosticAdmissionBlocked,
			wantSummary: "node reservation capacity exhausted",
		},
		{
			name:        "plain node selection",
			message:     "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=runtime_unsupported",
			wantCode:    DiagnosticNodeSelectionError,
			wantSummary: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DiagnosticCode(tt.message); got != tt.wantCode {
				t.Fatalf("DiagnosticCode() = %q, want %q", got, tt.wantCode)
			}
			if got := AdmissionBlockedSummary(tt.message); got != tt.wantSummary {
				t.Fatalf("AdmissionBlockedSummary() = %q, want %q", got, tt.wantSummary)
			}
		})
	}
}
