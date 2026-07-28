package runtimetest

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func TestFakeRuntimeHandlerDefaultShape(t *testing.T) {
	handler := NewFakeRuntimeHandler()
	if handler.Name() != config.RuntimeNameRunsc {
		t.Fatalf("expected default fake runtime name %q, got %q", config.RuntimeNameRunsc, handler.Name())
	}
	if !handler.Capabilities().CanCheckpoint {
		t.Fatalf("expected fake handler to advertise checkpoint support")
	}
	if len(handler.Requirements().Resources) != 0 {
		t.Fatalf("expected fake handler default resources to be empty, got %+v", handler.Requirements())
	}
}
