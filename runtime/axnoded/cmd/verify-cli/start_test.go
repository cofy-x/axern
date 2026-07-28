package main

import (
	"testing"
	"time"
)

func TestDefaultCreateSandboxTimeout(t *testing.T) {
	if defaultCreateSandboxTimeout != 90*time.Second {
		t.Fatalf("defaultCreateSandboxTimeout = %s, want 90s", defaultCreateSandboxTimeout)
	}
}
