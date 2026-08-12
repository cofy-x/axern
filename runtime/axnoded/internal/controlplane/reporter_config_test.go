package controlplane

import (
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestNewReporterReturnsNilWhenControlPlaneDisabled(t *testing.T) {
	r := NewReporter(
		"",
		"node-a",
		"127.0.0.1:25000",
		"node-token",
		"",
		"",
		"",
		5*time.Second,
		func() []string { return []string{"runsc"} },
		func() (nodeinventory.NodeInventorySnapshot, bool) { return nodeinventory.NewSnapshot(), true },
		func(nodeinventory.NodeInventorySnapshot) *nodev1.NodeSummary { return &nodev1.NodeSummary{} },
		nil,
	)
	if r != nil {
		t.Fatal("expected nil reporter when target is empty")
	}
}
