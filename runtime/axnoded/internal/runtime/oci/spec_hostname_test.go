package oci

import "testing"

func TestShortAllocationIdentity(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "opaque allocation id",
			value: "alloc-7f7c9509-212d-4368-a959-391d9d88a59c",
			want:  "7f7c9509-212",
		},
		{
			name:  "uppercase opaque allocation id",
			value: "ALLOC-7F7C9509-212D-4368-A959-391D9D88A59C",
			want:  "7f7c9509-212",
		},
		{
			name:  "readable allocation id",
			value: "claude-code-profile",
			want:  "claude-code",
		},
		{
			name:  "long readable allocation id",
			value: "this-is-a-very-long-readable-allocation-name",
			want:  "this-is-a-ve",
		},
		{
			name:  "invalid characters",
			value: "Claude Code/Profile",
			want:  "claude-code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortAllocationIdentity(tt.value); got != tt.want {
				t.Fatalf("shortAllocationIdentity(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
