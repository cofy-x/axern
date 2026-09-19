package runkernel

import (
	"testing"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestIsWatchCompleteWaitsForRequestedRootfsSnapshot(t *testing.T) {
	terminal := &runv1.Run{Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED}
	if !IsWatchComplete(terminal) {
		t.Fatal("terminal Run without a snapshot should complete its watch")
	}
	terminal.RootfsSnapshot = &runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING}
	if IsWatchComplete(terminal) {
		t.Fatal("terminal Run with a pending snapshot completed its watch")
	}
	terminal.RootfsSnapshot.Status = runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY
	if !IsWatchComplete(terminal) {
		t.Fatal("ready snapshot should complete its watch")
	}
	terminal.RootfsSnapshot.Status = runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_FAILED
	if !IsWatchComplete(terminal) {
		t.Fatal("failed snapshot should complete its watch")
	}
}

func TestIsWatchCompleteRequiresTerminalWorkload(t *testing.T) {
	run := &runv1.Run{
		Status:         runv1.RunStatus_RUN_STATUS_RUNNING,
		RootfsSnapshot: &runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY},
	}
	if IsWatchComplete(run) {
		t.Fatal("non-terminal Run completed its watch")
	}
}
