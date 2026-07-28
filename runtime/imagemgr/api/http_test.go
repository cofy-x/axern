package api

import (
	"testing"
)

func TestSplitObject(t *testing.T) {
	cases := []struct {
		name       string
		object     string
		wantPrefix string
		wantName   string
		wantErr    bool
	}{
		{name: "single-segment", object: "foo", wantPrefix: "", wantName: "foo"},
		{name: "nested", object: "dir/sub/file", wantPrefix: "dir/sub/", wantName: "file"},
		{name: "leading-slash", object: "/dir/file", wantPrefix: "/dir/", wantName: "file"},
		{name: "dot-clean", object: "dir/./file", wantPrefix: "dir/", wantName: "file"},
		{name: "trailing-slash", object: "dir/", wantErr: true},
		{name: "empty", object: "", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			prefix, name, err := splitObject(tc.object)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if prefix != tc.wantPrefix || name != tc.wantName {
				t.Fatalf("got prefix=%q name=%q, want prefix=%q name=%q", prefix, name, tc.wantPrefix, tc.wantName)
			}
		})
	}
}

const testImageURL = "registry.example.com/axern/client:test"

func TestOCIMountRequestString(t *testing.T) {
	req := OCIMountRequest{ImageURL: testImageURL}
	got := req.String()
	want := "(" + testImageURL + ")"
	if got != want {
		t.Errorf("OCIMountRequest.String() = %q, want %q", got, want)
	}
}

func TestOCIUmountRequestString(t *testing.T) {
	req := OCIUmountRequest{ImageURL: testImageURL}
	got := req.String()
	want := "(" + testImageURL + ")"
	if got != want {
		t.Errorf("OCIUmountRequest.String() = %q, want %q", got, want)
	}
}
