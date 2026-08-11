package appfunction

import (
	"context"
	"strings"
	"testing"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestInvokeFunctionDispatchesAndRecordsSuccess(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	store := &fakeFunctionStore{
		function: &functionv1.Function{
			ID:               "fn-1",
			Namespace:        "default",
			Name:             "hello",
			ActiveRevisionID: "fnrev-1",
			Spec:             &functionv1.FunctionSpec{Runtime: "python3.11"},
		},
		revision: &functionv1.FunctionRevision{ID: "fnrev-1", FunctionID: "fn-1"},
		deployment: &functionv1.FunctionDeployment{
			FunctionID:       "fn-1",
			ActiveRevisionID: "fnrev-1",
			WorkerServiceID:  "svc-1",
			DesiredReplicas:  1,
			ReadyReplicas:    1,
			Status:           functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY,
		},
	}
	invoker := &fakeFunctionInvoker{result: &functionv1.FunctionResult{ContentType: "application/json", Data: []byte(`{"ok":true}`)}}
	controller := NewController(ControllerDeps{Store: store, Invoker: invoker})

	resp, err := controller.InvokeFunction(context.Background(), &functionv1.InvokeFunctionRequest{
		Name:    "hello",
		Mode:    functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_SYNC,
		Payload: &functionv1.FunctionPayload{ContentType: "application/json", Data: []byte(`{"name":"Axern"}`)},
	}, now)
	if err != nil {
		t.Fatalf("InvokeFunction() error = %v", err)
	}
	if !invoker.called {
		t.Fatal("invoker was not called")
	}
	if store.finishedStatus != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED {
		t.Fatalf("finished status = %s, want SUCCEEDED", store.finishedStatus)
	}
	if got := resp.GetInvocation().GetResult().GetData(); string(got) != `{"ok":true}` {
		t.Fatalf("result data = %s, want success payload", string(got))
	}
}

func TestInvokeFunctionDoesNotDispatchToNonReadyWorker(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	store := readyFunctionStore()
	store.deployment.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_WARMING
	store.deployment.ReadyReplicas = 0
	invoker := &fakeFunctionInvoker{}
	controller := NewController(ControllerDeps{Store: store, Invoker: invoker})

	response, err := controller.InvokeFunction(context.Background(), &functionv1.InvokeFunctionRequest{
		Name: "hello",
		Mode: functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_SYNC,
	}, now)
	if err != nil {
		t.Fatalf("InvokeFunction() error = %v", err)
	}
	if invoker.called {
		t.Fatal("invoker was called for a non-ready deployment")
	}
	if got := response.GetInvocation().GetError().GetCode(); got != "worker_not_ready" {
		t.Fatalf("invocation error code = %q, want worker_not_ready", got)
	}
}

func TestGetFunctionRefreshesDeploymentFromWorkerService(t *testing.T) {
	store := &fakeFunctionStore{
		function: &functionv1.Function{
			ID:               "fn-1",
			Namespace:        "default",
			Name:             "hello",
			ActiveRevisionID: "fnrev-1",
			Spec:             &functionv1.FunctionSpec{Runtime: "python3.11"},
		},
		revision: &functionv1.FunctionRevision{ID: "fnrev-1", FunctionID: "fn-1"},
		deployment: &functionv1.FunctionDeployment{
			FunctionID:       "fn-1",
			ActiveRevisionID: "fnrev-1",
			WorkerServiceID:  "svc-1",
			DesiredReplicas:  1,
			Status:           functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_WARMING,
		},
	}
	services := fakeWorkerServiceController{
		service: &servicev1.Service{
			ID:            "svc-1",
			Replicas:      1,
			ReadyReplicas: 1,
			Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
		},
	}
	controller := NewController(ControllerDeps{Store: store, Services: services})

	resp, err := controller.GetFunction(context.Background(), &functionv1.GetFunctionRequest{FunctionID: "fn-1"})
	if err != nil {
		t.Fatalf("GetFunction() error = %v", err)
	}
	if resp.GetDeployment().GetStatus() != functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY {
		t.Fatalf("deployment status = %s, want READY", resp.GetDeployment().GetStatus())
	}
	if resp.GetDeployment().GetReadyReplicas() != 1 {
		t.Fatalf("ready replicas = %d, want 1", resp.GetDeployment().GetReadyReplicas())
	}
	if resp.GetFunction().GetStatus() != functionv1.FunctionStatus_FUNCTION_STATUS_READY || resp.GetFunction().GetDeploymentStatus() != functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY {
		t.Fatalf("function status = %s/%s, want READY/READY", resp.GetFunction().GetStatus(), resp.GetFunction().GetDeploymentStatus())
	}
}

func TestGetDeletedFunctionPreservesTerminalDeployment(t *testing.T) {
	store := &fakeFunctionStore{
		function: &functionv1.Function{
			ID:               "fn-1",
			Namespace:        "default",
			Name:             "hello",
			ActiveRevisionID: "fnrev-1",
			Status:           functionv1.FunctionStatus_FUNCTION_STATUS_DELETED,
			DeploymentStatus: functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO,
		},
		revision: &functionv1.FunctionRevision{ID: "fnrev-1", FunctionID: "fn-1"},
		deployment: &functionv1.FunctionDeployment{
			FunctionID:       "fn-1",
			ActiveRevisionID: "fnrev-1",
			WorkerServiceID:  "svc-1",
			DesiredReplicas:  0,
			ReadyReplicas:    0,
			Status:           functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO,
		},
	}
	services := fakeWorkerServiceController{service: &servicev1.Service{
		ID:            "svc-1",
		Replicas:      1,
		ReadyReplicas: 0,
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DELETED,
	}}
	controller := NewController(ControllerDeps{Store: store, Services: services})

	resp, err := controller.GetFunction(context.Background(), &functionv1.GetFunctionRequest{FunctionID: "fn-1"})
	if err != nil {
		t.Fatalf("GetFunction() error = %v", err)
	}
	if resp.GetDeployment().GetStatus() != functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO {
		t.Fatalf("deployment status = %s, want SCALED_TO_ZERO", resp.GetDeployment().GetStatus())
	}
	if resp.GetDeployment().GetDesiredReplicas() != 0 || resp.GetDeployment().GetReadyReplicas() != 0 {
		t.Fatalf("deployment replicas = %d/%d, want 0/0", resp.GetDeployment().GetReadyReplicas(), resp.GetDeployment().GetDesiredReplicas())
	}
}

func TestInvokeFunctionMapsResourceExhaustedDispatchError(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	store := readyFunctionStore()
	controller := NewController(ControllerDeps{
		Store:   store,
		Invoker: &fakeFunctionInvoker{err: grpcstatus.Error(codes.ResourceExhausted, "too large")},
	})

	resp, err := controller.InvokeFunction(context.Background(), &functionv1.InvokeFunctionRequest{
		Name:    "hello",
		Mode:    functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_SYNC,
		Payload: &functionv1.FunctionPayload{ContentType: "application/json", Data: []byte(`{"name":"Axern"}`)},
	}, now)
	if err != nil {
		t.Fatalf("InvokeFunction() error = %v", err)
	}
	if resp.GetInvocation().GetStatus() != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_FAILED {
		t.Fatalf("status = %s, want FAILED", resp.GetInvocation().GetStatus())
	}
	if resp.GetInvocation().GetError().GetCode() != "payload_too_large" {
		t.Fatalf("function error = %+v", resp.GetInvocation().GetError())
	}
}

func TestInvokeFunctionMapsDeadlineExceededDispatchErrorToTimedOut(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	store := readyFunctionStore()
	controller := NewController(ControllerDeps{
		Store:   store,
		Invoker: &fakeFunctionInvoker{err: grpcstatus.Error(codes.DeadlineExceeded, "deadline exceeded")},
	})

	resp, err := controller.InvokeFunction(context.Background(), &functionv1.InvokeFunctionRequest{
		Name:    "hello",
		Mode:    functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_SYNC,
		Payload: &functionv1.FunctionPayload{ContentType: "application/json", Data: []byte(`{"name":"Axern"}`)},
	}, now)
	if err != nil {
		t.Fatalf("InvokeFunction() error = %v", err)
	}
	if resp.GetInvocation().GetStatus() != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_TIMED_OUT {
		t.Fatalf("status = %s, want TIMED_OUT", resp.GetInvocation().GetStatus())
	}
}

func TestInvokeFunctionWarmsScaledToZeroWorker(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	store := readyFunctionStore()
	store.function.Spec.Scaling = &functionv1.FunctionScalingSpec{MinReplicas: 0, MaxReplicas: 10}
	store.deployment.DesiredReplicas = 0
	store.deployment.ReadyReplicas = 0
	store.deployment.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO
	services := &scalingWorkerServiceController{service: &servicev1.Service{
		ID: "svc-1", Replicas: 0, Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
	}}
	invoker := &fakeFunctionInvoker{result: &functionv1.FunctionResult{ContentType: "text/plain", Data: []byte("ok")}}
	controller := NewController(ControllerDeps{Store: store, Services: services, Invoker: invoker})

	resp, err := controller.InvokeFunction(context.Background(), &functionv1.InvokeFunctionRequest{
		Name: "hello", Mode: functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_SYNC,
	}, now)
	if err != nil {
		t.Fatalf("InvokeFunction() error = %v", err)
	}
	if services.updates != 1 {
		t.Fatalf("worker service updates = %d, want 1", services.updates)
	}
	if !invoker.called || resp.GetInvocation().GetStatus() != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED {
		t.Fatalf("invoker called = %v, invocation = %+v", invoker.called, resp.GetInvocation())
	}
}

func TestScaleDownWorkerRestoresServiceWhenInvocationWins(t *testing.T) {
	store := readyFunctionStore()
	store.scaleDownRecorded = false
	services := &scalingWorkerServiceController{service: &servicev1.Service{
		ID: "svc-1", Replicas: 1, ReadyReplicas: 1, Version: 7,
		Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
	}}
	controller := NewController(ControllerDeps{Store: store, Services: services})

	err := controller.scaleDownWorker(context.Background(), functionkernel.IdleDeployment{
		FunctionID: "fn-1", WorkerServiceID: "svc-1", DesiredReplicas: 1, MinReplicas: 0,
	}, time.Now())
	if err != nil {
		t.Fatalf("scaleDownWorker() error = %v", err)
	}
	if got := services.replicaUpdates; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("replica updates = %v, want [0 1]", got)
	}
	if got := services.expectedVersions; len(got) != 2 || got[0] != 7 || got[1] != 8 {
		t.Fatalf("expected versions = %v, want [7 8]", got)
	}
}

func TestAsyncInvokeReturnsQueuedStatus(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	store := readyFunctionStore()
	invoker := &fakeFunctionInvoker{result: &functionv1.FunctionResult{ContentType: "text/plain", Data: []byte("ok")}}
	controller := NewController(ControllerDeps{Store: store, Invoker: invoker})

	resp, err := controller.InvokeFunction(context.Background(), &functionv1.InvokeFunctionRequest{
		Name:    "hello",
		Mode:    functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC,
		Payload: &functionv1.FunctionPayload{ContentType: "text/plain", Data: []byte("hi")},
	}, now)
	if err != nil {
		t.Fatalf("InvokeFunction() error = %v", err)
	}
	if resp.GetInvocation().GetStatus() != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_QUEUED {
		t.Fatalf("status = %s, want QUEUED", resp.GetInvocation().GetStatus())
	}
}

func TestAsyncDispatcherCompletesDurableClaim(t *testing.T) {
	store := readyFunctionStore()
	store.invocation = functionkernel.NewInvocation(store.function, store.revision, functionkernel.InvokeParams{
		Mode:    functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC,
		Timeout: durationpb.New(time.Minute),
	}, time.Now())
	claim := &functionkernel.AsyncInvocationClaim{
		Invocation: store.invocation, Function: store.function, Revision: store.revision, Deployment: store.deployment,
		Owner: "owner", LeaseToken: "token", ExecutionGeneration: 1, Attempt: 1, DeadlineRemaining: time.Minute,
	}
	invoker := &fakeFunctionInvoker{result: &functionv1.FunctionResult{Data: []byte("ok")}}
	controller := NewController(ControllerDeps{Store: store, Invoker: invoker})

	controller.executeAsyncClaim(context.Background(), claim)

	if !invoker.called || store.finishedStatus != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED {
		t.Fatalf("invoker called=%t finished status=%s", invoker.called, store.finishedStatus)
	}
}

func TestAsyncDispatcherRequeuesTransientDispatchFailure(t *testing.T) {
	store := readyFunctionStore()
	store.invocation = functionkernel.NewInvocation(store.function, store.revision, functionkernel.InvokeParams{
		Mode:    functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC,
		Timeout: durationpb.New(time.Minute),
	}, time.Now())
	claim := &functionkernel.AsyncInvocationClaim{
		Invocation: store.invocation, Function: store.function, Revision: store.revision, Deployment: store.deployment,
		Owner: "owner", LeaseToken: "token", ExecutionGeneration: 1, Attempt: 1, DeadlineRemaining: time.Minute,
	}
	controller := NewController(ControllerDeps{Store: store, Invoker: &fakeFunctionInvoker{err: grpcstatus.Error(codes.Unavailable, "gateway unavailable")}})

	controller.executeAsyncClaim(context.Background(), claim)

	if !store.requeued || store.finishedStatus != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_UNSPECIFIED {
		t.Fatalf("requeued=%t finished status=%s", store.requeued, store.finishedStatus)
	}
}

func TestAsyncDispatcherRejectsSuccessAfterDeadline(t *testing.T) {
	store := readyFunctionStore()
	store.invocation = functionkernel.NewInvocation(store.function, store.revision, functionkernel.InvokeParams{
		Mode:    functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC,
		Timeout: durationpb.New(time.Minute),
	}, time.Now())
	claim := &functionkernel.AsyncInvocationClaim{
		Invocation: store.invocation, Function: store.function, Revision: store.revision, Deployment: store.deployment,
		Owner: "owner", LeaseToken: "token", ExecutionGeneration: 1, Attempt: 1,
		DeadlineRemaining: 10 * time.Millisecond,
	}
	controller := NewController(ControllerDeps{Store: store, Invoker: &fakeFunctionInvoker{
		result: &functionv1.FunctionResult{Data: []byte("late")},
		delay:  25 * time.Millisecond,
	}})

	controller.executeAsyncClaim(context.Background(), claim)

	if store.finishedStatus != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_TIMED_OUT {
		t.Fatalf("finished status=%s, want TIMED_OUT", store.finishedStatus)
	}
}

func TestInvokeAutoGeneratesRequestID(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	store := readyFunctionStore()
	invoker := &fakeFunctionInvoker{result: &functionv1.FunctionResult{ContentType: "text/plain", Data: []byte("ok")}}
	controller := NewController(ControllerDeps{Store: store, Invoker: invoker})

	resp, err := controller.InvokeFunction(context.Background(), &functionv1.InvokeFunctionRequest{
		Name:    "hello",
		Mode:    functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_SYNC,
		Payload: &functionv1.FunctionPayload{ContentType: "text/plain", Data: []byte("hi")},
	}, now)
	if err != nil {
		t.Fatalf("InvokeFunction() error = %v", err)
	}
	requestID := resp.GetInvocation().GetRequestID()
	if !strings.HasPrefix(requestID, "auto-") {
		t.Fatalf("request_id = %q, want prefix auto-", requestID)
	}
}

func TestWorkerConfigUsesRuntimeBundleDownloadPath(t *testing.T) {
	controller := NewController(ControllerDeps{
		BundleBaseURL: "http://controld.local:24001/",
		BundleToken:   "secret",
	})
	fn := &functionv1.Function{
		ID:   "fn-1",
		Name: "hello",
		Spec: &functionv1.FunctionSpec{
			Runtime:     "python3.11",
			Handler:     "handler.hello",
			Initializer: "handler.init",
		},
	}
	revision := &functionv1.FunctionRevision{
		ID: "fnrev-1",
		Source: &functionv1.FunctionSource{
			Source: &functionv1.FunctionSource_Bundle{Bundle: &functionv1.FunctionBundleSource{
				Digest:     "sha256:abc",
				StorageUri: "axern://function-bundles/abc.tar",
			}},
		},
	}

	cfg := controller.workerConfig(fn, revision, workerContract{
		Command:    []string{"python3", "-m", "axern_sdk.function.worker"},
		PortName:   "function-http",
		Port:       8080,
		HealthPath: "/healthz",
		InvokePath: "/invoke",
	})

	if got := cfg.GetEnv()["AXERN_FUNCTION_BUNDLE_URL"]; got != "http://controld.local:24001/runtime/function-bundles/abc.tar" {
		t.Fatalf("bundle url = %q", got)
	}
	if got := cfg.GetEnv()["AXERN_FUNCTION_BUNDLE_TOKEN"]; got != "secret" {
		t.Fatalf("bundle token = %q", got)
	}
}

func TestResolveWorkerEnvironmentUsesDeclaredTemplate(t *testing.T) {
	environments := &recordingEnvironmentControl{}
	controller := NewController(ControllerDeps{Environments: environments})
	fn := &functionv1.Function{
		Namespace: "team-a",
		Spec: &functionv1.FunctionSpec{WorkerSource: &functionv1.FunctionWorkerSource{
			Source: &functionv1.FunctionWorkerSource_Environment{Environment: &environmentv1.EnvironmentSpec{TemplateID: "function-python"}},
		}},
	}

	env, err := controller.resolveWorkerEnvironment(context.Background(), fn, nil, time.Now())
	if err != nil {
		t.Fatalf("resolveWorkerEnvironment() error = %v", err)
	}
	if env.GetNamespace() != "team-a" || environments.created.GetTemplateID() != "function-python" {
		t.Fatalf("environment = %#v, created spec = %#v", env, environments.created)
	}
}

func TestResolveWorkerEnvironmentRejectsCrossNamespaceEnvironment(t *testing.T) {
	environments := &recordingEnvironmentControl{existing: &environmentv1.Environment{
		ID: "env-a", Namespace: "team-b", Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY,
	}}
	controller := NewController(ControllerDeps{Environments: environments})
	fn := &functionv1.Function{
		Namespace: "team-a",
		Spec: &functionv1.FunctionSpec{WorkerSource: &functionv1.FunctionWorkerSource{
			Source: &functionv1.FunctionWorkerSource_EnvironmentID{EnvironmentID: "env-a"},
		}},
	}

	if _, err := controller.resolveWorkerEnvironment(context.Background(), fn, nil, time.Now()); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("resolveWorkerEnvironment() error = %v, want FailedPrecondition", err)
	}
}

func TestRolloutWorkerCreatesScaledToZeroServiceDirectly(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	store := readyFunctionStore()
	store.function.Namespace = "default"
	store.function.Spec = &functionv1.FunctionSpec{
		Runtime: "python3.11",
		Handler: "handler.hello",
		Scaling: &functionv1.FunctionScalingSpec{MinReplicas: 0, MaxReplicas: 1},
		WorkerSource: &functionv1.FunctionWorkerSource{Source: &functionv1.FunctionWorkerSource_Environment{
			Environment: &environmentv1.EnvironmentSpec{TemplateID: "python311"},
		}},
	}
	store.revision.Source = &functionv1.FunctionSource{Source: &functionv1.FunctionSource_Bundle{Bundle: &functionv1.FunctionBundleSource{
		Digest: "sha256:abc", StorageUri: "axern://function-bundles/abc.tar",
	}}}
	store.deployment.WorkerServiceID = ""
	services := &recordingWorkerServiceController{}
	controller := NewController(ControllerDeps{
		Store:        store,
		Environments: &recordingEnvironmentControl{},
		Services:     services,
	})

	_, deployment, err := controller.rolloutWorker(context.Background(), store.function, store.revision, store.deployment, now)
	if err != nil {
		t.Fatalf("rolloutWorker() error = %v", err)
	}
	if services.created == nil || services.created.Replicas != 0 {
		t.Fatalf("created service params = %+v, want replicas=0", services.created)
	}
	if services.updates != 0 {
		t.Fatalf("service updates = %d, want 0", services.updates)
	}
	if deployment.GetWorkerServiceID() != "svc-created" || deployment.GetDesiredReplicas() != 0 {
		t.Fatalf("deployment = %+v, want scaled-to-zero worker service", deployment)
	}
}

type fakeFunctionInvoker struct {
	called bool
	result *functionv1.FunctionResult
	fnErr  *functionv1.FunctionError
	err    error
	delay  time.Duration
}

func readyFunctionStore() *fakeFunctionStore {
	return &fakeFunctionStore{
		function: &functionv1.Function{
			ID:               "fn-1",
			Namespace:        "default",
			Name:             "hello",
			ActiveRevisionID: "fnrev-1",
			Spec:             &functionv1.FunctionSpec{Runtime: "python3.11"},
		},
		revision: &functionv1.FunctionRevision{ID: "fnrev-1", FunctionID: "fn-1"},
		deployment: &functionv1.FunctionDeployment{
			FunctionID:       "fn-1",
			ActiveRevisionID: "fnrev-1",
			WorkerServiceID:  "svc-1",
			DesiredReplicas:  1,
			ReadyReplicas:    1,
			Status:           functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY,
		},
	}
}

func (f *fakeFunctionInvoker) InvokeFunctionWorker(context.Context, FunctionInvokeDispatch) (*functionv1.FunctionResult, *functionv1.FunctionError, error) {
	f.called = true
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return f.result, f.fnErr, f.err
}

type fakeFunctionStore struct {
	function          *functionv1.Function
	revision          *functionv1.FunctionRevision
	deployment        *functionv1.FunctionDeployment
	invocation        *functionv1.FunctionInvocation
	finishedStatus    functionv1.FunctionInvocationStatus
	scaleDownRecorded bool
	requeued          bool
}

func (f *fakeFunctionStore) SaveBundle(context.Context, functionkernel.UploadBundleParams, time.Time) (*functionv1.FunctionBundleSource, error) {
	panic("unexpected SaveBundle")
}

func (f *fakeFunctionStore) DeployFunction(context.Context, functionkernel.DeployParams, time.Time) (*functionkernel.DeployResult, error) {
	panic("unexpected DeployFunction")
}

func (f *fakeFunctionStore) AttachWorkerService(_ context.Context, _, _, serviceID string, desiredReplicas int32, _ time.Time) (*functionv1.Function, *functionv1.FunctionDeployment, bool, error) {
	f.deployment.WorkerServiceID = serviceID
	f.deployment.DesiredReplicas = desiredReplicas
	f.deployment.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_WARMING
	return f.function, f.deployment, true, nil
}

func (f *fakeFunctionStore) StartInvocation(_ context.Context, params functionkernel.InvokeParams, now time.Time) (*functionkernel.InvocationStartResult, error) {
	f.invocation = functionkernel.NewInvocation(f.function, f.revision, params, now)
	return &functionkernel.InvocationStartResult{
		Invocation: f.invocation,
		Function:   f.function,
		Revision:   f.revision,
		Deployment: f.deployment,
		Found:      true,
	}, nil
}

func (f *fakeFunctionStore) FinishInvocation(_ context.Context, invocationID string, status functionv1.FunctionInvocationStatus, result *functionv1.FunctionResult, fnErr *functionv1.FunctionError, message string, now time.Time) (*functionv1.FunctionInvocation, bool, error) {
	f.finishedStatus = status
	return functionkernel.FinishInvocation(f.invocation, status, result, fnErr, message, now), true, nil
}

func (f *fakeFunctionStore) GetFunction(context.Context, string, string, string) (*functionv1.Function, *functionv1.FunctionRevision, *functionv1.FunctionDeployment, bool, error) {
	return f.function, f.revision, f.deployment, true, nil
}

func (f *fakeFunctionStore) ListFunctions(context.Context, *functionv1.FunctionListFilter) ([]*functionv1.Function, string, error) {
	panic("unexpected ListFunctions")
}

func (f *fakeFunctionStore) DeleteFunction(context.Context, string, string, string, time.Time) (*functionv1.Function, bool, error) {
	panic("unexpected DeleteFunction")
}

func (f *fakeFunctionStore) GetInvocation(context.Context, string) (*functionv1.FunctionInvocation, bool, error) {
	panic("unexpected GetInvocation")
}

func (f *fakeFunctionStore) ListInvocations(context.Context, *functionv1.FunctionInvocationListFilter) ([]*functionv1.FunctionInvocation, string, error) {
	panic("unexpected ListInvocations")
}

func (f *fakeFunctionStore) ListEvents(context.Context, string, string, string, int32) ([]*functionv1.FunctionEvent, error) {
	panic("unexpected ListEvents")
}

func (f *fakeFunctionStore) ListIdleDeployments(context.Context, time.Time) ([]functionkernel.IdleDeployment, error) {
	return nil, nil
}

func (f *fakeFunctionStore) RecordScaleDown(context.Context, string, int32, time.Time) (bool, error) {
	return f.scaleDownRecorded, nil
}

func (f *fakeFunctionStore) ClaimAsyncInvocation(context.Context, string, time.Duration) (*functionkernel.AsyncInvocationClaim, bool, error) {
	return nil, false, nil
}

func (f *fakeFunctionStore) RenewAsyncInvocation(context.Context, *functionkernel.AsyncInvocationClaim, time.Duration) (bool, error) {
	return true, nil
}

func (f *fakeFunctionStore) RequeueAsyncInvocation(context.Context, *functionkernel.AsyncInvocationClaim, time.Duration, string) (bool, error) {
	f.requeued = true
	return true, nil
}

func (f *fakeFunctionStore) FinishAsyncInvocation(_ context.Context, _ *functionkernel.AsyncInvocationClaim, status functionv1.FunctionInvocationStatus, result *functionv1.FunctionResult, fnErr *functionv1.FunctionError, message string) (*functionv1.FunctionInvocation, bool, error) {
	f.finishedStatus = status
	return functionkernel.FinishInvocation(f.invocation, status, result, fnErr, message, time.Now()), true, nil
}

func (f *fakeFunctionStore) ExpireAsyncInvocations(context.Context, int) (int, error) {
	return 0, nil
}

func (f *fakeFunctionStore) WaitForAsyncInvocation(ctx context.Context, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type fakeWorkerServiceController struct {
	service *servicev1.Service
}

type recordingWorkerServiceController struct {
	created *servicekernel.CreateParams
	updates int
}

func (r *recordingWorkerServiceController) Create(_ context.Context, params servicekernel.CreateParams, _ time.Time) (*servicev1.Service, error) {
	r.created = &params
	return &servicev1.Service{ID: "svc-created", Replicas: params.Replicas, Status: servicev1.ServiceStatus_SERVICE_STATUS_READY}, nil
}

func (*recordingWorkerServiceController) Get(context.Context, string) (*servicev1.Service, bool, error) {
	return nil, false, nil
}

func (r *recordingWorkerServiceController) Update(context.Context, *servicev1.UpdateServiceRequest, time.Time) (*servicev1.Service, error) {
	r.updates++
	return nil, nil
}

func (*recordingWorkerServiceController) Delete(context.Context, servicekernel.DeleteParams, time.Time) (*servicev1.Service, bool, error) {
	return nil, false, nil
}

type scalingWorkerServiceController struct {
	service          *servicev1.Service
	updates          int
	replicaUpdates   []int32
	expectedVersions []int64
}

func (*scalingWorkerServiceController) Create(context.Context, servicekernel.CreateParams, time.Time) (*servicev1.Service, error) {
	panic("unexpected Create")
}

func (f *scalingWorkerServiceController) Get(context.Context, string) (*servicev1.Service, bool, error) {
	return f.service, f.service != nil, nil
}

func (f *scalingWorkerServiceController) Update(_ context.Context, req *servicev1.UpdateServiceRequest, _ time.Time) (*servicev1.Service, error) {
	f.updates++
	f.replicaUpdates = append(f.replicaUpdates, req.GetReplicas())
	f.expectedVersions = append(f.expectedVersions, req.GetExpectedVersion())
	f.service.Replicas = req.GetReplicas()
	f.service.ReadyReplicas = req.GetReplicas()
	f.service.Status = servicev1.ServiceStatus_SERVICE_STATUS_READY
	f.service.Version++
	return f.service, nil
}

func (*scalingWorkerServiceController) Delete(context.Context, servicekernel.DeleteParams, time.Time) (*servicev1.Service, bool, error) {
	panic("unexpected Delete")
}

func (fakeWorkerServiceController) Create(context.Context, servicekernel.CreateParams, time.Time) (*servicev1.Service, error) {
	panic("unexpected Create")
}

func (f fakeWorkerServiceController) Get(context.Context, string) (*servicev1.Service, bool, error) {
	if f.service == nil {
		return nil, false, nil
	}
	return f.service, true, nil
}

func (fakeWorkerServiceController) Update(context.Context, *servicev1.UpdateServiceRequest, time.Time) (*servicev1.Service, error) {
	panic("unexpected Update")
}

func (fakeWorkerServiceController) Delete(context.Context, servicekernel.DeleteParams, time.Time) (*servicev1.Service, bool, error) {
	panic("unexpected Delete")
}

type fakeEnvironmentCreator struct{}

func (fakeEnvironmentCreator) CreateEnvironment(context.Context, *environmentv1.EnvironmentSpec, map[string]string, time.Time) (*environmentv1.Environment, error) {
	panic("unexpected CreateEnvironment")
}

func (fakeEnvironmentCreator) GetEnvironment(context.Context, string) (*environmentv1.Environment, error) {
	panic("unexpected GetEnvironment")
}

var _ FunctionInvoker = (*fakeFunctionInvoker)(nil)
var _ functionkernel.Store = (*fakeFunctionStore)(nil)
var _ WorkerServiceController = fakeWorkerServiceController{}
var _ WorkerServiceController = (*scalingWorkerServiceController)(nil)
var _ WorkerServiceController = (*recordingWorkerServiceController)(nil)
var _ EnvironmentControl = fakeEnvironmentCreator{}

type recordingEnvironmentControl struct {
	created  *environmentv1.EnvironmentSpec
	existing *environmentv1.Environment
}

func (r *recordingEnvironmentControl) CreateEnvironment(_ context.Context, spec *environmentv1.EnvironmentSpec, _ map[string]string, _ time.Time) (*environmentv1.Environment, error) {
	r.created = spec
	return &environmentv1.Environment{ID: "env-created", Namespace: spec.GetNamespace(), Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY, Spec: spec}, nil
}

func (r *recordingEnvironmentControl) GetEnvironment(context.Context, string) (*environmentv1.Environment, error) {
	return r.existing, nil
}
