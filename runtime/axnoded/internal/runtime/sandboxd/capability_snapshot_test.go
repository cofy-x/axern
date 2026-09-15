package sandboxd

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func TestSnapshotFromDiagnosticsExplainsUnavailableProvider(t *testing.T) {
	snapshot := SnapshotFromDiagnostics("/tmp/sandboxd.sock", wire.DiagnosticsResponse{
		Ready:        true,
		Status:       wire.StatusResponse{UserProcess: wire.UserProcessStatus{State: "running"}},
		Capabilities: []string{"file", "process"},
		Providers: []wire.CapabilityProvider{
			{
				Name:         "computer_use",
				State:        "unavailable",
				Available:    false,
				Reason:       "display_server unavailable",
				Capabilities: []string{"computer_use"},
				Dependencies: []wire.ProviderDependency{{Name: "display_server", Available: false, Reason: "not found"}},
			},
		},
	})

	err := snapshot.RequireCapability("computer_use")
	if !errord.IsFailedPrecondition(err) {
		t.Fatalf("RequireCapability(computer_use) error = %v, want failed precondition", err)
	}
	if !strings.Contains(err.Error(), "display_server unavailable") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("RequireCapability(computer_use) error = %v, want provider reason and dependency detail", err)
	}
}

func TestTargetForSocketUsesExplicitDerivedEndpoint(t *testing.T) {
	target := TargetForSocket(" /tmp/sandboxd.sock ")
	if target.Client == nil || target.SocketPath != "/tmp/sandboxd.sock" {
		t.Fatalf("target = %#v", target)
	}
}

func TestSnapshotFromReadySortsCapabilities(t *testing.T) {
	snapshot := SnapshotFromReady("/tmp/sandboxd.sock", wire.ReadySnapshot{
		Status: wire.StatusResponse{UserProcess: wire.UserProcessStatus{State: "running"}},
		Capabilities: wire.CapabilitiesResponse{
			Capabilities: []string{"process", "file", "archive"},
			Providers: []wire.CapabilityProvider{
				{Name: "file", Available: true, Capabilities: []string{"file", "archive"}},
			},
		},
	})
	got := snapshot.CapabilityList()
	want := []string{"archive", "file", "process"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CapabilityList() = %#v, want %#v", got, want)
		}
	}
	if _, ok := snapshot.Providers["file"]; !ok {
		t.Fatalf("providers = %#v", snapshot.Providers)
	}
}

func TestSnapshotFromDiagnosticsRequiresControlReady(t *testing.T) {
	snapshot := SnapshotFromDiagnostics("/tmp/sandboxd.sock", wire.DiagnosticsResponse{
		Ready:        false,
		Status:       wire.StatusResponse{UserProcess: wire.UserProcessStatus{State: "starting"}},
		Capabilities: []string{"status"},
	})
	if err := snapshot.RequireReady(); !errord.IsFailedPrecondition(err) {
		t.Fatalf("RequireReady() error = %v, want failed precondition", err)
	}
}

func TestSnapshotFromDiagnosticsAllowsStartingUserProcessWhenControlReady(t *testing.T) {
	snapshot := SnapshotFromDiagnostics("/tmp/sandboxd.sock", wire.DiagnosticsResponse{
		Ready:        true,
		Status:       wire.StatusResponse{UserProcess: wire.UserProcessStatus{State: "starting"}},
		Capabilities: []string{"status"},
	})
	if err := snapshot.RequireReady(); err != nil {
		t.Fatalf("RequireReady() error = %v", err)
	}
	if snapshot.UserState != "starting" {
		t.Fatalf("user state = %q, want starting", snapshot.UserState)
	}
}
