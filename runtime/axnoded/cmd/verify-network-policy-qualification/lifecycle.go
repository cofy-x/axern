package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	privatenodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"google.golang.org/grpc/status"
)

// Capture before test-owned cleanup, without exposing raw verifier errors,
// selected proofs, policy contents, or destination names in CI logs.
func dumpAllocationDiagnostics(sample int, clients *verifyutil.NodeClients, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := verifyutil.GetAllocationLifecycle(ctx, clients, id)
	diagnostic := allocationDiagnostic(sample, response)
	diagnostic["rpc_code"] = status.Code(err).String()
	_ = json.NewEncoder(os.Stderr).Encode(diagnostic)
}

func allocationDiagnostic(sample int, response *privatenodev1.GetAllocationLifecycleResponse) map[string]any {
	conditions := make([]map[string]string, 0)
	for _, condition := range response.GetCapabilityVerification().GetConditions() {
		conditions = append(conditions, map[string]string{
			"platform":    condition.GetKey().GetPlatform().String(),
			"state":       condition.GetState().String(),
			"reason_code": condition.GetReasonCode().String(),
		})
	}
	var exitCode *int32
	if response != nil {
		exitCode = response.ExitCode
	}
	return map[string]any{
		"kind": "allocation_failure", "sample": sample,
		"status": response.GetState().String(), "exit_code": exitCode,
		"conditions": conditions,
	}
}
