package function

import (
	"testing"

	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
)

func TestFunctionReady(t *testing.T) {
	tests := []struct {
		name       string
		function   functionv1.FunctionStatus
		deployment functionv1.FunctionDeploymentStatus
		desired    int32
		ready      int32
		want       bool
	}{
		{"ready replicas", functionv1.FunctionStatus_FUNCTION_STATUS_READY, functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY, 2, 2, true},
		{"scaled to zero", functionv1.FunctionStatus_FUNCTION_STATUS_READY, functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO, 0, 0, true},
		{"ready deployment without replicas", functionv1.FunctionStatus_FUNCTION_STATUS_READY, functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY, 1, 0, false},
		{"deploying function", functionv1.FunctionStatus_FUNCTION_STATUS_DEPLOYING, functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &functionv1.GetFunctionResponse{
				Function: &functionv1.Function{Status: tt.function},
				Deployment: &functionv1.FunctionDeployment{
					Status:          tt.deployment,
					DesiredReplicas: tt.desired,
					ReadyReplicas:   tt.ready,
				},
			}
			if got := functionReady(resp); got != tt.want {
				t.Fatalf("functionReady() = %v, want %v", got, tt.want)
			}
		})
	}
}
