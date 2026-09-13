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
				Name:         "browser",
				State:        "unavailable",
				Available:    false,
				Reason:       "browser_command unavailable",
				Capabilities: []string{"browser"},
				Dependencies: []wire.ProviderDependency{{Name: "browser_command", Available: false, Reason: "not found"}},
			},
		},
	})

	err := snapshot.RequireCapability("browser")
	if !errord.IsFailedPrecondition(err) {
		t.Fatalf("RequireCapability(browser) error = %v, want failed precondition", err)
	}
	if !strings.Contains(err.Error(), "browser_command unavailable") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("RequireCapability(browser) error = %v, want provider reason and dependency detail", err)
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
