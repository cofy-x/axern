package oci

import "testing"

func TestShortServiceIdentity(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "opaque service id",
			value: "svc-7f7c9509-212d-4368-a959-391d9d88a59c",
			want:  "svc-7f7c9509",
		},
		{
			name:  "uppercase opaque service id",
			value: "SVC-7F7C9509-212D-4368-A959-391D9D88A59C",
			want:  "svc-7f7c9509",
		},
		{
			name:  "readable service id",
			value: "claude-code-profile",
			want:  "claude-code-profile",
		},
		{
			name:  "long readable service id",
			value: "this-is-a-very-long-readable-service-name",
			want:  "this-is-a-very-long-readable-ser",
		},
		{
			name:  "invalid characters",
			value: "Claude Code/Profile",
			want:  "claude-code-profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortServiceIdentity(tt.value); got != tt.want {
				t.Fatalf("shortServiceIdentity(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
