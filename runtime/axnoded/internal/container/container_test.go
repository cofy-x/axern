package container

import (
	"encoding/json"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func TestContainer_EnvValue(t *testing.T) {
	type fields struct {
		Metadata *apipb.ContainerMetadata
		Status   StatusStorage
		Spec     *spec.Spec
		PATH     string
	}
	type args struct {
		key string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   string
	}{
		{
			name: "test",
			fields: fields{
				Spec: &spec.Spec{
					Process: &spec.Process{
						Env: []string{"a=1", "b=2"},
					},
				},
			},
			args: args{
				key: "a",
			},
			want: "1",
		},
		{
			name: "empty container",
			fields: fields{
				Spec: &spec.Spec{},
			},
			args: args{
				key: "a",
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Container{
				Metadata: tt.fields.Metadata,
				Status:   tt.fields.Status,
				Spec:     tt.fields.Spec,
				PATH:     tt.fields.PATH,
			}
			if got := c.EnvValue(tt.args.key); got != tt.want {
				t.Errorf("EnvValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_ApiStatus(t *testing.T) {
	type fields struct {
		Metadata *apipb.ContainerMetadata
		Status   StatusStorage
		Spec     *spec.Spec
		PATH     string
	}
	tests := []struct {
		name   string
		fields fields
		want   *runtime.ContainerStatus
	}{
		{
			name: "test",
			fields: fields{
				Metadata: &apipb.ContainerMetadata{
					ID:             "123",
					RuntimeHandler: "runsc",
					Labels: map[string]string{
						"test": "test",
					},
					Stdout: "/root/stdout",
					Stderr: "/root/stderr",
				},
				Status: &statusStorage{
					status: Status{
						Pid:       123,
						StartedAt: "202308201132",
					},
				},
				Spec: &spec.Spec{
					Process: &spec.Process{
						Args: []string{"a", "b"},
						Env:  []string{"a=1", "b=2"},
					},
				},
				PATH: "/root",
			},
			want: &runtime.ContainerStatus{
				ID:        "123",
				Command:   []string{"a", "b"},
				Runtime:   "runsc",
				Stdout:    "/root/stdout",
				Stderr:    "/root/stderr",
				ExitCode:  0,
				StartedAt: 202308201132,
				Pid:       123,
				Labels: map[string]string{
					"test": "test",
				},
				Mounts: []*runtime.Mount{},
				State:  runtime.ContainerState_CONTAINER_RUNNING,
				Envs: []*runtime.KeyValue{
					{
						Key:   "a",
						Value: "1",
					},
					{
						Key:   "b",
						Value: "2",
					},
				},
			},
		},
		{
			name: "test for empty status",
			fields: fields{
				Metadata: &apipb.ContainerMetadata{
					ID:             "123",
					RuntimeHandler: "runsc",
					Labels: map[string]string{
						"test": "test",
					},
					Stdout: "/root/stdout",
					Stderr: "/root/stderr",
				},
				Status: nil,
				Spec: &spec.Spec{
					Process: &spec.Process{
						Args: []string{"a", "b"},
						Env:  []string{"a=1", "b=2"},
					},
				},
				PATH: "/root",
			},
			want: &runtime.ContainerStatus{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Container{
				Metadata: tt.fields.Metadata,
				Status:   tt.fields.Status,
				Spec:     tt.fields.Spec,
				PATH:     tt.fields.PATH,
			}
			// compare the fields of the struct by json.Marshal
			got := c.ApiStatus()
			b1, e1 := json.Marshal(got)
			b2, e2 := json.Marshal(tt.want)
			if e1 != nil || e2 != nil {
				t.Errorf("ApiStatus() error = %v, wantErr %v", e1, e2)
			}
			if string(b1) != string(b2) {
				t.Errorf("ApiStatus() = %v\nwant %v", string(b1), string(b2))
			}
		})
	}
}
