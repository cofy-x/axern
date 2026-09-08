package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc/status"
)

// Capture before test-owned cleanup, without exposing raw verifier errors,
// selected proofs, policy contents, or destination names in CI logs.
func dumpAllocationDiagnostics(sample int, clients *verifyutil.NodeClients, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := verifyutil.GetAllocationStatus(ctx, clients, id)
	diagnostic := allocationDiagnostic(sample, response)
	diagnostic["rpc_code"] = status.Code(err).String()
	_ = json.NewEncoder(os.Stderr).Encode(diagnostic)
}

func allocationDiagnostic(sample int, response *privatenodev1.GetAllocationStatusResponse) map[string]any {
	conditions := make([]map[string]string, 0)
	for _, condition := range response.GetCapabilityVerification().GetConditions() {
		conditions = append(conditions, map[string]string{
			"platform":    condition.GetKey().GetPlatform().String(),
			"state":       condition.GetState().String(),
			"reason_code": condition.GetReasonCode().String(),
		})
	}
	return map[string]any{
		"kind": "allocation_failure", "sample": sample,
		"status": response.GetStatus().String(), "exit_code": response.GetExitCode(),
		"exit_code_known": response.GetExitCodeKnown(), "conditions": conditions,
	}
}
