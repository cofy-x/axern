package sandboxd

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func TestSnapshotFromLabelsRequiresCapability(t *testing.T) {
	snapshot, err := SnapshotFromLabels(map[string]string{
		LabelReady:        "true",
		LabelSocket:       "/tmp/sandboxd.sock",
		LabelCapabilities: " process, file,archive ",
		LabelUserState:    "running",
	})
	if err != nil {
		t.Fatalf("SnapshotFromLabels() error = %v", err)
	}
	if snapshot.SocketPath != "/tmp/sandboxd.sock" || snapshot.UserState != "running" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := snapshot.RequireCapability("file"); err != nil {
		t.Fatalf("RequireCapability(file) error = %v", err)
	}
	if err := snapshot.RequireCapability("browser"); !errord.IsFailedPrecondition(err) {
		t.Fatalf("RequireCapability(browser) error = %v, want failed precondition", err)
	}
}

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

func TestSnapshotFromLabelsRequiresReady(t *testing.T) {
	snapshot, err := SnapshotFromLabels(map[string]string{
		LabelReady:        "false",
		LabelSocket:       "/tmp/sandboxd.sock",
		LabelCapabilities: "status",
	})
	if err != nil {
		t.Fatalf("SnapshotFromLabels() error = %v", err)
	}
	if err := snapshot.RequireReady(); !errord.IsFailedPrecondition(err) {
		t.Fatalf("RequireReady() error = %v, want failed precondition", err)
	}
}

func TestSnapshotFromLabelsRejectsMissingMetadata(t *testing.T) {
	if _, err := SnapshotFromLabels(nil); !errord.IsFailedPrecondition(err) {
		t.Fatalf("SnapshotFromLabels(nil) error = %v, want failed precondition", err)
	}
	if _, err := SnapshotFromLabels(map[string]string{LabelReady: "true"}); !errord.IsFailedPrecondition(err) {
		t.Fatalf("SnapshotFromLabels(missing socket) error = %v, want failed precondition", err)
	}
}

func TestTargetFromLabelsBuildsClientAfterCapabilityCheck(t *testing.T) {
	target, err := TargetFromLabels(map[string]string{
		LabelReady:        "true",
		LabelSocket:       "/tmp/sandboxd.sock",
		LabelCapabilities: "file,process",
	}, "file")
	if err != nil {
		t.Fatalf("TargetFromLabels() error = %v", err)
	}
	if target.Client == nil || target.Snapshot.SocketPath != "/tmp/sandboxd.sock" {
		t.Fatalf("target = %#v", target)
	}
	if _, err := TargetFromLabels(map[string]string{
		LabelReady:        "true",
		LabelSocket:       "/tmp/sandboxd.sock",
		LabelCapabilities: "file",
	}, "process"); !errord.IsFailedPrecondition(err) {
		t.Fatalf("TargetFromLabels() error = %v, want failed precondition", err)
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
