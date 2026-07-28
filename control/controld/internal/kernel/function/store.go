package functionkernel

import (
	"context"
	"errors"
	"time"

	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

var errNotImplemented = errors.New("function control is not implemented")
var errNotFound = errors.New("function resource not found")

func NotImplemented() error {
	return errNotImplemented
}

func IsNotImplemented(err error) bool {
	return errors.Is(err, errNotImplemented)
}

func NotFound() error {
	return errNotFound
}

func IsNotFound(err error) bool {
	return errors.Is(err, errNotFound)
}

type Control interface {
	UploadFunctionBundle(ctx context.Context, params UploadBundleParams, now time.Time) (*functionv1.UploadFunctionBundleResponse, error)
	DeployFunction(ctx context.Context, req *functionv1.DeployFunctionRequest, now time.Time) (*functionv1.DeployFunctionResponse, error)
	GetFunction(ctx context.Context, req *functionv1.GetFunctionRequest) (*functionv1.GetFunctionResponse, error)
	ListFunctions(ctx context.Context, req *functionv1.ListFunctionsRequest) (*functionv1.ListFunctionsResponse, error)
	DeleteFunction(ctx context.Context, req *functionv1.DeleteFunctionRequest, now time.Time) (*functionv1.DeleteFunctionResponse, error)
	InvokeFunction(ctx context.Context, req *functionv1.InvokeFunctionRequest, now time.Time) (*functionv1.InvokeFunctionResponse, error)
	GetFunctionInvocation(ctx context.Context, req *functionv1.GetFunctionInvocationRequest) (*functionv1.GetFunctionInvocationResponse, error)
	ListFunctionInvocations(ctx context.Context, req *functionv1.ListFunctionInvocationsRequest) (*functionv1.ListFunctionInvocationsResponse, error)
	ListFunctionEvents(ctx context.Context, req *functionv1.ListFunctionEventsRequest) (*functionv1.ListFunctionEventsResponse, error)
}

type DeployParams struct {
	Namespace string
	Name      string
	Spec      *functionv1.FunctionSpec
	Source    *functionv1.FunctionSource
	Labels    map[string]string
}

type UploadBundleParams struct {
	Namespace string
	Name      string
	Digest    string
	MediaType string
	SizeBytes int64
	Payload   []byte
}

type DeployResult struct {
	Function   *functionv1.Function
	Revision   *functionv1.FunctionRevision
	Deployment *functionv1.FunctionDeployment
}

type InvocationStartResult struct {
	Invocation *functionv1.FunctionInvocation
	Function   *functionv1.Function
	Revision   *functionv1.FunctionRevision
	Deployment *functionv1.FunctionDeployment
	Found      bool
	Replay     bool
}

type Store interface {
	SaveBundle(ctx context.Context, params UploadBundleParams, now time.Time) (*functionv1.FunctionBundleSource, error)
	DeployFunction(ctx context.Context, params DeployParams, now time.Time) (*DeployResult, error)
	AttachWorkerService(ctx context.Context, functionID, revisionID, serviceID string, desiredReplicas int32, now time.Time) (*functionv1.Function, *functionv1.FunctionDeployment, bool, error)
	StartInvocation(ctx context.Context, params InvokeParams, now time.Time) (*InvocationStartResult, error)
	FinishInvocation(ctx context.Context, invocationID string, status functionv1.FunctionInvocationStatus, result *functionv1.FunctionResult, fnErr *functionv1.FunctionError, message string, now time.Time) (*functionv1.FunctionInvocation, bool, error)
	GetFunction(ctx context.Context, functionID, namespace, name string) (*functionv1.Function, *functionv1.FunctionRevision, *functionv1.FunctionDeployment, bool, error)
	ListFunctions(ctx context.Context, filter *functionv1.FunctionListFilter) ([]*functionv1.Function, string, error)
	DeleteFunction(ctx context.Context, functionID, namespace, name string, now time.Time) (*functionv1.Function, bool, error)
	GetInvocation(ctx context.Context, invocationID string) (*functionv1.FunctionInvocation, bool, error)
	ListInvocations(ctx context.Context, filter *functionv1.FunctionInvocationListFilter) ([]*functionv1.FunctionInvocation, string, error)
	ListEvents(ctx context.Context, functionID, invocationID, revisionID string, limit int32) ([]*functionv1.FunctionEvent, error)
	ListIdleDeployments(ctx context.Context, now time.Time) ([]IdleDeployment, error)
	RecordScaleDown(ctx context.Context, functionID string, replicas int32, now time.Time) (bool, error)
	ClaimAsyncInvocation(ctx context.Context, owner string, leaseTTL time.Duration) (*AsyncInvocationClaim, bool, error)
	RenewAsyncInvocation(ctx context.Context, claim *AsyncInvocationClaim, leaseTTL time.Duration) (bool, error)
	RequeueAsyncInvocation(ctx context.Context, claim *AsyncInvocationClaim, delay time.Duration, message string) (bool, error)
	FinishAsyncInvocation(ctx context.Context, claim *AsyncInvocationClaim, status functionv1.FunctionInvocationStatus, result *functionv1.FunctionResult, fnErr *functionv1.FunctionError, message string) (*functionv1.FunctionInvocation, bool, error)
	ExpireAsyncInvocations(ctx context.Context, limit int) (int, error)
	WaitForAsyncInvocation(ctx context.Context, safetyTimeout time.Duration) error
}

type AsyncInvocationClaim struct {
	Invocation          *functionv1.FunctionInvocation
	Function            *functionv1.Function
	Revision            *functionv1.FunctionRevision
	Deployment          *functionv1.FunctionDeployment
	Owner               string
	LeaseToken          string
	ExecutionGeneration int64
	Attempt             int
	DeadlineRemaining   time.Duration
}

type IdleDeployment struct {
	FunctionID      string
	Namespace       string
	FunctionName    string
	WorkerServiceID string
	IdleTimeout     time.Duration
	LastInvokedAt   time.Time
	DesiredReplicas int32
	MinReplicas     int32
}

type InvokeParams struct {
	Namespace  string
	Name       string
	FunctionID string
	RevisionID string
	Mode       functionv1.FunctionInvocationMode
	Payload    *functionv1.FunctionPayload
	Timeout    *durationpb.Duration
	RequestID  string
	Labels     map[string]string
}
