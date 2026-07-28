package function

import (
	"context"

	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
)

func (c Control) Get(ctx context.Context, functionID, namespace, name string) (*functionv1.GetFunctionResponse, error) {
	return c.client.GetFunction(ctx, &functionv1.GetFunctionRequest{
		FunctionID: functionID,
		Namespace:  namespace,
		Name:       name,
	})
}

func (c Control) List(ctx context.Context, req *functionv1.ListFunctionsRequest) (*functionv1.ListFunctionsResponse, error) {
	return c.client.ListFunctions(ctx, req)
}

func (c Control) Delete(ctx context.Context, functionID, namespace, name string) (*functionv1.DeleteFunctionResponse, error) {
	return c.client.DeleteFunction(ctx, &functionv1.DeleteFunctionRequest{
		FunctionID: functionID,
		Namespace:  namespace,
		Name:       name,
	})
}

func (c Control) Invoke(ctx context.Context, req *functionv1.InvokeFunctionRequest) (*functionv1.InvokeFunctionResponse, error) {
	return c.client.InvokeFunction(ctx, req)
}

func (c Control) GetInvocation(ctx context.Context, invocationID string) (*functionv1.GetFunctionInvocationResponse, error) {
	return c.client.GetFunctionInvocation(ctx, &functionv1.GetFunctionInvocationRequest{
		InvocationID: invocationID,
	})
}

func (c Control) ListInvocations(ctx context.Context, req *functionv1.ListFunctionInvocationsRequest) (*functionv1.ListFunctionInvocationsResponse, error) {
	return c.client.ListFunctionInvocations(ctx, req)
}

func (c Control) ListEvents(ctx context.Context, functionID string, limit int32) (*functionv1.ListFunctionEventsResponse, error) {
	return c.client.ListFunctionEvents(ctx, &functionv1.ListFunctionEventsRequest{
		FunctionID: functionID,
		Limit:      limit,
	})
}
