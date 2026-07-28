package oci

import "testing"

func TestBuildOverlayMountData(t *testing.T) {
	tests := []struct {
		name      string
		lowerDirs []string
		want      string
		wantErr   bool
	}{
		{
			name:      "single lowerdir",
			lowerDirs: []string{"/tmp/layers/l1/fs"},
			want:      "lowerdir=/tmp/layers/l1/fs",
		},
		{
			name:      "multiple lowerdirs",
			lowerDirs: []string{"/tmp/layers/l2/fs", "/tmp/layers/l1/fs"},
			want:      "lowerdir=/tmp/layers/l2/fs:/tmp/layers/l1/fs",
		},
		{
			name:      "empty lowerdirs",
			lowerDirs: nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildOverlayMountData(tt.lowerDirs)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildOverlayMountData() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildOverlayMountData() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("buildOverlayMountData() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMountLowerDirsAppendsSupportDirLast(t *testing.T) {
	mgr := newTestManager(t)
	lowerDirs := mgr.buildMountLowerDirs([]string{"/layers/l2", "/layers/l1"})

	if len(lowerDirs) != 3 {
		t.Fatalf("buildMountLowerDirs() len = %d, want 3", len(lowerDirs))
	}
	if lowerDirs[0] != "/layers/l2" || lowerDirs[1] != "/layers/l1" {
		t.Fatalf("buildMountLowerDirs() reordered chain lowerdirs: %+v", lowerDirs)
	}
	if lowerDirs[2] != mgr.supportDir {
		t.Fatalf("buildMountLowerDirs() support dir = %q, want %q", lowerDirs[2], mgr.supportDir)
	}
}
