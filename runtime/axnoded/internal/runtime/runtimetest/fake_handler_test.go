package runtimetest

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func TestFakeSandboxRuntimeDefaultShape(t *testing.T) {
	handler := NewFakeSandboxRuntime()
	if handler.Name() != config.RuntimeNameRunsc {
		t.Fatalf("expected default fake runtime name %q, got %q", config.RuntimeNameRunsc, handler.Name())
	}
	if len(handler.HostRequirements().Resources) != 0 {
		t.Fatalf("expected fake handler default resources to be empty, got %+v", handler.HostRequirements())
	}
}
