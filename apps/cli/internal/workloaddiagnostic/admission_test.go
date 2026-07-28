package workloaddiagnostic

import "testing"

func TestAdmissionBlocked(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{
			name:    "namespace quota",
			message: "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a",
			want:    true,
		},
		{
			name:    "reservation capacity",
			message: "no node has remaining reservation capacity: node_id=node-a effective_allocatable_milli=2000 available_milli=0",
			want:    true,
		},
		{
			name:    "placement CPU capacity",
			message: "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=insufficient_cpu",
			want:    true,
		},
		{
			name:    "placement memory capacity",
			message: "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=insufficient_memory",
			want:    true,
		},
		{
			name:    "plain no eligible node",
			message: "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=runtime_unsupported",
			want:    false,
		},
		{
			name:    "empty",
			message: "",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AdmissionBlocked(tt.message); got != tt.want {
				t.Fatalf("AdmissionBlocked(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}

func TestAdmissionBlockedSummary(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "namespace quota",
			message: "namespace quota exceeded: namespace=team-a",
			want:    "namespace quota exceeded",
		},
		{
			name:    "CPU capacity",
			message: "no eligible node: rejection_reasons=insufficient_cpu",
			want:    "node CPU capacity exhausted",
		},
		{
			name:    "memory capacity",
			message: "no eligible node: rejection_reasons=insufficient_memory",
			want:    "node memory capacity exhausted",
		},
		{
			name:    "CPU and memory capacity",
			message: "no eligible node: rejection_reasons=insufficient_cpu,insufficient_memory",
			want:    "node CPU and memory capacity exhausted",
		},
		{
			name:    "node selection",
			message: "no eligible node: rejection_reasons=runtime_unsupported",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AdmissionBlockedSummary(tt.message); got != tt.want {
				t.Fatalf("AdmissionBlockedSummary(%q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestDiagnosticCode(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "quota admission",
			message: "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a",
			want:    DiagnosticAdmissionBlocked,
		},
		{
			name:    "capacity placement admission",
			message: "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=insufficient_memory",
			want:    DiagnosticAdmissionBlocked,
		},
		{
			name:    "plain node selection",
			message: "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=runtime_unsupported",
			want:    DiagnosticNodeSelectionError,
		},
		{
			name:    "storage topology",
			message: "service volume topology unsatisfied: no placement candidates satisfy required volume topology",
			want:    DiagnosticStorageTopologyUnsatisfied,
		},
		{
			name:    "storage reserve",
			message: "storage reserve failed: volume binding reserve requires claim, class, and mount",
			want:    DiagnosticStorageReserveError,
		},
		{
			name:    "volume spec conflict",
			message: "storage reserve failed: volume binding \"alloc-a/data\" already exists with a different resolved volume",
			want:    DiagnosticVolumeSpecConflict,
		},
		{
			name:    "volume publish",
			message: "volume publish failed: volumed: volume does not support runtime class \"runsc\"",
			want:    DiagnosticVolumePublishError,
		},
		{
			name:    "volume release",
			message: "volume release failed: unpublish volume alloc-a/data: input/output error",
			want:    DiagnosticVolumeReleaseError,
		},
		{
			name:    "other message",
			message: "runtime start failed",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DiagnosticCode(tt.message); got != tt.want {
				t.Fatalf("DiagnosticCode(%q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}
