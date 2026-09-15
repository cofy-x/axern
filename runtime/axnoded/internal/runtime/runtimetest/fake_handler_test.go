package runtimetest

import (
	"testing"
)

func TestFakeSandboxRuntimeDefaultShape(t *testing.T) {
	handler := NewFakeSandboxRuntime()
	if len(handler.HostRequirements().Resources) != 0 {
		t.Fatalf("expected fake handler default resources to be empty, got %+v", handler.HostRequirements())
	}
}
