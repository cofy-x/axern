package appfunction

import (
	"context"

	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

// FunctionInvoker is the application boundary to the gateway-owned worker
// dispatch path. The Function controller owns invocation state and audit; the
// implementation owns resolving and forwarding to the selected worker.
type FunctionInvoker interface {
	InvokeFunctionWorker(ctx context.Context, req FunctionInvokeDispatch) (*functionv1.FunctionResult, *functionv1.FunctionError, error)
}

type FunctionInvokeDispatch struct {
	Function   *functionv1.Function
	Revision   *functionv1.FunctionRevision
	Deployment *functionv1.FunctionDeployment
	Invocation *functionv1.FunctionInvocation
	Payload    *functionv1.FunctionPayload
	Timeout    *durationpb.Duration
}
