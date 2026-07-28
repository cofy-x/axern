package function

import (
	"context"

	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"google.golang.org/grpc"
)

type FunctionClient interface {
	UploadFunctionBundle(ctx context.Context, opts ...grpc.CallOption) (functionv1.FunctionControl_UploadFunctionBundleClient, error)
	DeployFunction(ctx context.Context, in *functionv1.DeployFunctionRequest, opts ...grpc.CallOption) (*functionv1.DeployFunctionResponse, error)
	GetFunction(ctx context.Context, in *functionv1.GetFunctionRequest, opts ...grpc.CallOption) (*functionv1.GetFunctionResponse, error)
	ListFunctions(ctx context.Context, in *functionv1.ListFunctionsRequest, opts ...grpc.CallOption) (*functionv1.ListFunctionsResponse, error)
	DeleteFunction(ctx context.Context, in *functionv1.DeleteFunctionRequest, opts ...grpc.CallOption) (*functionv1.DeleteFunctionResponse, error)
	InvokeFunction(ctx context.Context, in *functionv1.InvokeFunctionRequest, opts ...grpc.CallOption) (*functionv1.InvokeFunctionResponse, error)
	GetFunctionInvocation(ctx context.Context, in *functionv1.GetFunctionInvocationRequest, opts ...grpc.CallOption) (*functionv1.GetFunctionInvocationResponse, error)
	ListFunctionInvocations(ctx context.Context, in *functionv1.ListFunctionInvocationsRequest, opts ...grpc.CallOption) (*functionv1.ListFunctionInvocationsResponse, error)
	ListFunctionEvents(ctx context.Context, in *functionv1.ListFunctionEventsRequest, opts ...grpc.CallOption) (*functionv1.ListFunctionEventsResponse, error)
}

type Control struct {
	client FunctionClient
}

func New(client FunctionClient) Control {
	return Control{client: client}
}
