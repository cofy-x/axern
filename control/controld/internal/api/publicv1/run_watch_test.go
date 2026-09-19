package publicv1

import (
	"context"
	"io"
	"testing"
	"time"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc/metadata"
)

func TestWatchRunStreamsPendingThenReadySnapshot(t *testing.T) {
	runs := &watchRunStore{responses: []*runv1.Run{
		terminalSnapshotRun(2, runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING),
		terminalSnapshotRun(3, runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY),
	}}
	stream := &recordingRunWatchStream{ctx: context.Background()}
	if err := New(Dependencies{Runs: runs}).WatchRun(&runv1.WatchRunRequest{RunID: "run-a", AfterVersion: 1}, stream); err != nil {
		t.Fatal(err)
	}
	if len(stream.responses) != 2 || stream.responses[0].GetRun().GetRootfsSnapshot().GetStatus() != runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING || stream.responses[1].GetRun().GetRootfsSnapshot().GetStatus() != runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY {
		t.Fatalf("responses = %#v", stream.responses)
	}
	if len(runs.afterVersions) != 2 || runs.afterVersions[0] != 1 || runs.afterVersions[1] != 2 {
		t.Fatalf("after versions = %v, want [1 2]", runs.afterVersions)
	}
}

func TestWatchRunStreamsFailedSnapshotAsFinalUpdate(t *testing.T) {
	runs := &watchRunStore{responses: []*runv1.Run{
		terminalSnapshotRun(2, runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_FAILED),
	}}
	stream := &recordingRunWatchStream{ctx: context.Background()}
	if err := New(Dependencies{Runs: runs}).WatchRun(&runv1.WatchRunRequest{RunID: "run-a", AfterVersion: 1}, stream); err != nil {
		t.Fatal(err)
	}
	if len(stream.responses) != 1 || stream.responses[0].GetRun().GetRootfsSnapshot().GetStatus() != runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_FAILED {
		t.Fatalf("responses = %#v", stream.responses)
	}
}

func TestWatchRunTerminalWithoutSnapshotCompletesWithoutSyntheticUpdate(t *testing.T) {
	runs := &watchRunStore{responses: []*runv1.Run{{ID: "run-a", Version: 1, Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED}}}
	stream := &recordingRunWatchStream{ctx: context.Background()}
	if err := New(Dependencies{Runs: runs}).WatchRun(&runv1.WatchRunRequest{RunID: "run-a", AfterVersion: 1}, stream); err != nil {
		t.Fatal(err)
	}
	if len(stream.responses) != 0 || len(runs.afterVersions) != 1 {
		t.Fatalf("responses = %d calls = %v", len(stream.responses), runs.afterVersions)
	}
}

func terminalSnapshotRun(version int64, status runv1.RootfsSnapshotStatus) *runv1.Run {
	return &runv1.Run{ID: "run-a", Version: version, Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED, RootfsSnapshot: &runv1.RootfsSnapshotResult{Status: status}}
}

type watchRunStore struct {
	responses     []*runv1.Run
	afterVersions []int64
}

func (s *watchRunStore) WatchRun(_ context.Context, _ string, afterVersion int64) (*runv1.Run, error) {
	s.afterVersions = append(s.afterVersions, afterVersion)
	if len(s.responses) == 0 {
		return nil, io.EOF
	}
	run := s.responses[0]
	s.responses = s.responses[1:]
	return run, nil
}

func (*watchRunStore) CreateRun(context.Context, runkernel.CreateParams, time.Time) (*runv1.Run, error) {
	panic("unexpected CreateRun")
}
func (*watchRunStore) GetRun(context.Context, string) (*runv1.Run, error) {
	panic("unexpected GetRun")
}
func (*watchRunStore) ListRuns(context.Context, *runv1.RunListFilter) ([]*runv1.Run, string, error) {
	panic("unexpected ListRuns")
}
func (*watchRunStore) CancelRun(context.Context, string, time.Time) (*runv1.Run, error) {
	panic("unexpected CancelRun")
}

type recordingRunWatchStream struct {
	ctx       context.Context
	responses []*runv1.WatchRunResponse
}

func (s *recordingRunWatchStream) Send(response *runv1.WatchRunResponse) error {
	s.responses = append(s.responses, response)
	return nil
}
func (s *recordingRunWatchStream) Context() context.Context   { return s.ctx }
func (*recordingRunWatchStream) SetHeader(metadata.MD) error  { return nil }
func (*recordingRunWatchStream) SendHeader(metadata.MD) error { return nil }
func (*recordingRunWatchStream) SetTrailer(metadata.MD)       {}
func (*recordingRunWatchStream) SendMsg(any) error            { return nil }
func (*recordingRunWatchStream) RecvMsg(any) error            { return io.EOF }
