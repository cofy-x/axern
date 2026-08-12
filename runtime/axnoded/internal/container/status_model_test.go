package container

import (
	"reflect"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
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
			name: "exited inventory state is not terminal evidence",
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
				ExitCode:       0,
				ExitCodeKnown:  false,
				Message:        "",
				Unknown:        false,
				LinuxResources: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateStatusFromState(tt.args.state, tt.args.path).Get()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GenerateStatusFromState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateStatusFromStateExitedDoesNotInventFinishedAt(t *testing.T) {
	got := GenerateStatusFromState(&contract.UnionContainerState{
		InitProcessPid: 100,
		Status:         contract.ContainerStatusExited,
		Created:        "2023-08-28 16:34:07.878055688 +0800 CST m=+0.008551102",
	}, "/tmp").Get()

	if got.FinishedAt != "" || got.ExitCodeKnown || got.Message != "" {
		t.Fatalf("inventory state invented terminal evidence: %+v", got)
	}
}
