package api

import (
	"testing"
)

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
