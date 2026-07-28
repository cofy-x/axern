package container

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"reflect"
	"testing"
	"time"
)

func TestGenerateStatusFromState(t *testing.T) {
	type args struct {
		state *contract.UnionContainerState
		path  string
	}
	tests := []struct {
		name string
		args args
		want Status
	}{
		{
			name: "test",
			args: args{
				state: &contract.UnionContainerState{
					ID:             "",
					InitProcessPid: 100,
					Status:         "running",
					Bundle:         "",
					Created:        "2023-08-28 16:34:07.878055688 +0800 CST m=+0.008551102",
				},
				path: "/tmp",
			},
			want: Status{
				Pid:            100,
				StartedAt:      "2023-08-28 16:34:07.878055688 +0800 CST m=+0.008551102",
				FinishedAt:     "",
				ExitCode:       0,
				ExitCodeKnown:  false,
				Message:        "",
				Unknown:        false,
				LinuxResources: nil,
			},
		},
		{
			name: "exited without runtime exit status",
			args: args{
				state: &contract.UnionContainerState{
					InitProcessPid: 100,
					Status:         contract.ContainerStatusExited,
					Created:        "2023-08-28 16:34:07.878055688 +0800 CST m=+0.008551102",
				},
				path: "/tmp",
			},
			want: Status{
				Pid:            100,
				StartedAt:      "2023-08-28 16:34:07.878055688 +0800 CST m=+0.008551102",
				FinishedAt:     "",
				ExitCode:       -1,
				ExitCodeKnown:  false,
				Message:        unknownExitStatusMessage,
				Unknown:        false,
				LinuxResources: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateStatusFromState(tt.args.state, tt.args.path).Get()
			if tt.args.state.Status == contract.ContainerStatusExited {
				tt.want.FinishedAt = got.FinishedAt
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GenerateStatusFromState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateStatusFromStateExitedUsesRFC3339FinishedAt(t *testing.T) {
	got := GenerateStatusFromState(&contract.UnionContainerState{
		InitProcessPid: 100,
		Status:         contract.ContainerStatusExited,
		Created:        "2023-08-28 16:34:07.878055688 +0800 CST m=+0.008551102",
	}, "/tmp").Get()

	if _, err := time.Parse(time.RFC3339, got.FinishedAt); err != nil {
		t.Fatalf("FinishedAt = %q, want RFC3339 parseable time: %v", got.FinishedAt, err)
	}
}

func TestUpdateStatusByStatePreservesStartedAtWhenRuntimeOmitsCreated(t *testing.T) {
	updated := UpdateStatusByState(&contract.UnionContainerState{
		InitProcessPid: 104,
		Status:         contract.ContainerStatusRunning,
		Created:        "",
	}, Status{
		Pid:       0,
		StartedAt: "2026-04-23T10:58:51Z",
	})

	if updated.Pid != 104 {
		t.Fatalf("Pid = %d, want 104", updated.Pid)
	}
	if updated.StartedAt != "2026-04-23T10:58:51Z" {
		t.Fatalf("StartedAt = %q, want preserved timestamp", updated.StartedAt)
	}
}
