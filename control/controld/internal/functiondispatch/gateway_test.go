package functiondispatch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	appfunction "github.com/cofy-x/axern/control/controld/internal/application/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestGatewayInvokesFunctionWorker(t *testing.T) {
	t.Parallel()
	var gotPath, gotBody, gotAuth, gotNamespace, gotService, gotRequest, gotInvocation string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotNamespace = r.Header.Get(headerNamespace)
		gotService = r.Header.Get(headerWorkerServiceID)
		gotRequest = r.Header.Get(headerRequestID)
		gotInvocation = r.Header.Get(headerInvocationID)
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	gateway, err := NewGateway(GatewayConfig{URL: server.URL, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}

	result, fnErr, err := gateway.InvokeFunctionWorker(context.Background(), dispatchRequest())

	if err != nil || fnErr != nil {
		t.Fatalf("InvokeFunctionWorker() result error = %v function error = %+v", err, fnErr)
	}
	if gotPath != functionDispatchPath || gotBody != "hello" {
		t.Fatalf("request path/body = %q/%q", gotPath, gotBody)
	}
	if gotAuth != "Bearer secret" || gotNamespace != "default" || gotService != "svc-1" || gotRequest != "req-1" || gotInvocation != "inv-1" {
		t.Fatalf("headers auth=%q namespace=%q service=%q request=%q invocation=%q", gotAuth, gotNamespace, gotService, gotRequest, gotInvocation)
	}
	if result.GetContentType() != "application/json" || string(result.GetData()) != `{"ok":true}` {
		t.Fatalf("result = content_type:%q data:%q", result.GetContentType(), string(result.GetData()))
	}
}

func TestGatewayMapsWorkerHTTPErrorToFunctionError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	gateway, err := NewGateway(GatewayConfig{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	result, fnErr, err := gateway.InvokeFunctionWorker(context.Background(), dispatchRequest())

	if err != nil || result != nil {
		t.Fatalf("result = %+v err = %v", result, err)
	}
	if fnErr.GetCode() != "worker_http_500" || fnErr.GetDetails()["http_status"] != "500" {
		t.Fatalf("function error = %+v", fnErr)
	}
}

func TestGatewayMapsGatewayTimeoutHeaderToDeadlineExceeded(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(gatewayErrorClassHeader, "timeout")
		http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
	}))
	defer server.Close()
	gateway, err := NewGateway(GatewayConfig{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	result, fnErr, err := gateway.InvokeFunctionWorker(context.Background(), dispatchRequest())

	if result != nil || fnErr != nil {
		t.Fatalf("result = %+v function error = %+v", result, fnErr)
	}
	if grpcstatus.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("error code = %v, want %v", grpcstatus.Code(err), codes.DeadlineExceeded)
	}
}

func TestGatewayMapsBodyTooLargeHeaderToResourceExhausted(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(gatewayErrorClassHeader, "body_too_large")
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
	}))
	defer server.Close()
	gateway, err := NewGateway(GatewayConfig{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	result, fnErr, err := gateway.InvokeFunctionWorker(context.Background(), dispatchRequest())

	if result != nil || fnErr != nil {
		t.Fatalf("result = %+v function error = %+v", result, fnErr)
	}
	if grpcstatus.Code(err) != codes.ResourceExhausted {
		t.Fatalf("error code = %v, want %v", grpcstatus.Code(err), codes.ResourceExhausted)
	}
}

func TestGatewayRetriesGatewayLeaseError(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if calls.Add(1) == 1 {
			w.Header().Set(gatewayErrorClassHeader, gatewayLeaseErrorClass)
			http.Error(w, "lease not propagated", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	gateway, err := NewGateway(GatewayConfig{URL: server.URL, Timeout: 5 * leaseRetryBaseDelay})
	if err != nil {
		t.Fatal(err)
	}

	result, fnErr, err := gateway.InvokeFunctionWorker(context.Background(), dispatchRequest())

	if err != nil || fnErr != nil {
		t.Fatalf("InvokeFunctionWorker() result error = %v function error = %+v", err, fnErr)
	}
	if calls.Load() != 2 {
		t.Fatalf("gateway calls = %d, want 2", calls.Load())
	}
	if len(bodies) != 2 || bodies[0] != "hello" || bodies[1] != "hello" {
		t.Fatalf("request bodies = %#v", bodies)
	}
	if string(result.GetData()) != `{"ok":true}` {
		t.Fatalf("result data = %q", string(result.GetData()))
	}
}

func TestGatewayStopsRetryingLeaseErrorAfterAttempts(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set(gatewayErrorClassHeader, gatewayLeaseErrorClass)
		http.Error(w, "lease not propagated", http.StatusBadGateway)
	}))
	defer server.Close()
	gateway, err := NewGateway(GatewayConfig{URL: server.URL, Timeout: 5 * leaseRetryBaseDelay})
	if err != nil {
		t.Fatal(err)
	}

	result, fnErr, err := gateway.InvokeFunctionWorker(context.Background(), dispatchRequest())

	if result != nil || fnErr != nil {
		t.Fatalf("result = %+v function error = %+v", result, fnErr)
	}
	if grpcstatus.Code(err) != codes.Unavailable {
		t.Fatalf("error code = %v, want %v", grpcstatus.Code(err), codes.Unavailable)
	}
	if calls.Load() != leaseRetryAttempts {
		t.Fatalf("gateway calls = %d, want %d", calls.Load(), leaseRetryAttempts)
	}
}

func dispatchRequest() appfunction.FunctionInvokeDispatch {
	return appfunction.FunctionInvokeDispatch{
		Function: &functionv1.Function{
			ID:        "fn-1",
			Namespace: "default",
			Name:      "hello",
		},
		Revision:   &functionv1.FunctionRevision{ID: "rev-1"},
		Deployment: &functionv1.FunctionDeployment{WorkerServiceID: "svc-1"},
		Invocation: &functionv1.FunctionInvocation{ID: "inv-1", RequestID: "req-1"},
		Payload:    &functionv1.FunctionPayload{ContentType: "text/plain", Data: []byte("hello")},
	}
}
