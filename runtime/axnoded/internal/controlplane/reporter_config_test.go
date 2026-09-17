package controlplane

import (
	"testing"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
)

func TestNewReporterReturnsNilWhenControlPlaneDisabled(t *testing.T) {
	r := NewReporter(
		"",
		"node-a",
		"127.0.0.1:25000",
		nil,
		5*time.Second,
		func() (nodeinventory.NodeInventorySnapshot, bool) { return nodeinventory.NewSnapshot(), true },
		func(nodeinventory.NodeInventorySnapshot) *nodev1.NodeSummary { return &nodev1.NodeSummary{} },
		nil,
	)
	if r != nil {
		t.Fatal("expected nil reporter when target is empty")
	}
}
