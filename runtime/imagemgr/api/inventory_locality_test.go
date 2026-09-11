package api

import "testing"

func TestMountLocalityKeyUsesResolvedOCIIdentity(t *testing.T) {
	tests := []struct {
		name  string
		mount MountedImageDetail
		want  string
	}{
		{
			name: "resolved cache identity",
			mount: MountedImageDetail{
				ImageURL:  "docker.io/library/busybox@sha256:source",
				CacheKey:  "index.docker.io/library/busybox@sha256:resolved",
				MountType: MountTypeOCI,
			},
			want: "image:index.docker.io/library/busybox@sha256:resolved",
		},
		{
			name: "legacy image URL fallback",
			mount: MountedImageDetail{
				ImageURL:  "docker.io/library/busybox@sha256:source",
				MountType: MountTypeOCI,
			},
			want: "image:docker.io/library/busybox@sha256:source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mountLocalityKey(tt.mount)
			if !ok {
				t.Fatal("mountLocalityKey() ok = false, want true")
			}
			if got != tt.want {
				t.Fatalf("mountLocalityKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
