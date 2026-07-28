package main

import (
	"reflect"
	"testing"
)

func TestResolveCommandUsesDefaultWhenJSONEmpty(t *testing.T) {
	got, err := resolveCommand("")
	if err != nil {
		t.Fatalf("resolveCommand(empty) error = %v", err)
	}
	want := []string{"/bin/sh", "-c", "sleep 300"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveCommand(empty) = %v, want %v", got, want)
	}
}

func TestResolveCommandParsesJSONArgv(t *testing.T) {
	got, err := resolveCommand(`["/usr/sbin/nginx","-v"]`)
	if err != nil {
		t.Fatalf("resolveCommand(json) error = %v", err)
	}
	want := []string{"/usr/sbin/nginx", "-v"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveCommand(json) = %v, want %v", got, want)
	}
}

func TestResolveCommandRejectsEmptyJSONArgv(t *testing.T) {
	if _, err := resolveCommand(`[]`); err == nil {
		t.Fatal("resolveCommand(empty json) = nil error, want error")
	}
}
