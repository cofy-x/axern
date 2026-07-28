package cgroup

import "testing"

func TestParseCpusetCPUCount(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{input: "0", want: 1},
		{input: "0-3", want: 4},
		{input: "0-1,4,6-7", want: 5},
	}

	for _, tt := range tests {
		got, err := parseCpusetCPUCount(tt.input)
		if err != nil {
			t.Fatalf("parseCpusetCPUCount(%q) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parseCpusetCPUCount(%q)=%d want %d", tt.input, got, tt.want)
		}
	}
}
