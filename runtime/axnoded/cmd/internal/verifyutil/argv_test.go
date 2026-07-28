package verifyutil

import (
	"reflect"
	"testing"
)

func TestResolveArgvUsesDefaultShellSnippet(t *testing.T) {
	got, err := ResolveArgv("", "", "sleep 300")
	if err != nil {
		t.Fatalf("ResolveArgv returned error: %v", err)
	}
	want := []string{"/bin/sh", "-c", "sleep 300"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveArgv = %v, want %v", got, want)
	}
}

func TestResolveArgvPrefersJSONArgv(t *testing.T) {
	got, err := ResolveArgv(`["/usr/sbin/nginx","-v"]`, "", "ignored")
	if err != nil {
		t.Fatalf("ResolveArgv returned error: %v", err)
	}
	want := []string{"/usr/sbin/nginx", "-v"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveArgv = %v, want %v", got, want)
	}
}

func TestResolveArgvRejectsEmptyJSONArgv(t *testing.T) {
	if _, err := ResolveArgv(`[]`, "", ""); err == nil {
		t.Fatal("ResolveArgv should reject empty argv-json")
	}
}
