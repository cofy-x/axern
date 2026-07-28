package envelopepolicy

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestEligibleForStaticEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		request *apipb.StartRequest
		want    bool
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "matching static request",
			request: &apipb.StartRequest{
				RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runsc"},
			},
			want: true,
		},
		{
			name: "different runtime",
			request: &apipb.StartRequest{
				RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runc"},
			},
		},
		{
			name: "checkpoint restore is dynamic",
			request: &apipb.StartRequest{
				RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runsc"},
				CkptDir:         "/tmp/ckpt",
			},
		},
		{
			name: "network is dynamic",
			request: &apipb.StartRequest{
				RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runsc"},
				Network:         "bridge",
			},
		},
		{
			name: "extra env is dynamic",
			request: &apipb.StartRequest{
				RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runsc"},
				UserEnvs:        map[string]string{"A": "B"},
			},
		},
		{
			name: "resource is dynamic",
			request: &apipb.StartRequest{
				RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runsc"},
				Resources:       &commonv1.ResourceSpec{},
			},
		},
		{
			name: "network policy extra config",
			request: &apipb.StartRequest{
				RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runsc"},
				ExtraConfig:     `{"blockNetwork":true}`,
			},
		},
		{
			name: "request identity extra config",
			request: &apipb.StartRequest{
				RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runsc"},
				ExtraConfig:     `{"namespace":"default","serviceId":"svc-a"}`,
			},
		},
		{
			name: "invalid extra config is dynamic",
			request: &apipb.StartRequest{
				RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runsc"},
				ExtraConfig:     `{`,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := EligibleForStaticEnvelope(tt.request, "runsc")
			if got != tt.want {
				t.Fatalf("EligibleForStaticEnvelope() = %v, want %v", got, tt.want)
			}
		})
	}
}
