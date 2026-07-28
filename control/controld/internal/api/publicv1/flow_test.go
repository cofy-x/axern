package publicv1_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	publicv1 "github.com/cofy-x/axern/control/controld/internal/api/publicv1"
	app "github.com/cofy-x/axern/control/controld/internal/app"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

func TestCreateEnvironmentCreatesOwnedResourcesFromNormalizedSpec(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	first, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec:   &environmentv1.EnvironmentSpec{TemplateID: "python311", Namespace: "default"},
		Labels: map[string]string{"team": "infra"},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(first) error = %v", err)
	}
	second, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec:   &environmentv1.EnvironmentSpec{TemplateID: "python311", Namespace: "default"},
		Labels: map[string]string{"team": "runtime"},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(second) error = %v", err)
	}
	if first.GetEnvironment().GetID() == second.GetEnvironment().GetID() {
		t.Fatalf("independent creates shared environment ID %q", first.GetEnvironment().GetID())
	}
	if first.GetEnvironment().GetSpecHash() != second.GetEnvironment().GetSpecHash() {
		t.Fatalf("normalized spec hashes differ: %q != %q", first.GetEnvironment().GetSpecHash(), second.GetEnvironment().GetSpecHash())
	}
}

func TestCreateImageEnvironmentResolvesDigestForOwnedResources(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	first, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace: "default",
			Image:     &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"},
		},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(first image) error = %v", err)
	}
	second, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace: "default",
			Image:     &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"},
		},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(second image) error = %v", err)
	}
	if first.GetEnvironment().GetID() == second.GetEnvironment().GetID() {
		t.Fatalf("independent image-backed creates shared environment ID %q", first.GetEnvironment().GetID())
	}
	if first.GetEnvironment().GetSpecHash() != second.GetEnvironment().GetSpecHash() {
		t.Fatalf("resolved image spec hashes differ: %q != %q", first.GetEnvironment().GetSpecHash(), second.GetEnvironment().GetSpecHash())
	}
	if got := first.GetEnvironment().GetSpec().GetImage().GetDigest(); got == "" {
		t.Fatal("resolved image digest = empty, want resolved digest persisted")
	}
	if got := first.GetEnvironment().GetResolvedTemplate().GetImageDescriptor().GetDigest(); got != first.GetEnvironment().GetSpec().GetImage().GetDigest() {
		t.Fatalf("resolved template digest = %q, want %q", got, first.GetEnvironment().GetSpec().GetImage().GetDigest())
	}
}

func TestCreateEnvironmentRejectsInvalidImageSourceCombinations(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	_, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace:       "default",
			TemplateID:      "python311",
			TemplateVersion: "sha256:abc",
			Image:           &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"},
		},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("mixed source code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}

	_, err = public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{Namespace: "default"},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing source code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}

	_, err = public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace:       "default",
			TemplateVersion: "1",
			Image:           &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"},
		},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("image template_version code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}

	_, err = public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace: "default",
			Image: &environmentv1.EnvironmentImageSource{
				Ref:    "docker.io/library/nginx:1.27",
				Digest: "sha256:user-supplied",
			},
		},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("client digest code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
}

func TestCreateImageEnvironmentMutableTagCreatesNewEnvironmentWhenDigestChanges(t *testing.T) {
	resolver := controldtest.NewFakeImageResolver()
	service := newTestServiceWithImageResolver(t, resolver)
	defer service.Close()
	public := service.PublicV1Handler()

	first, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace: "default",
			Image:     &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"},
		},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(first mutable image) error = %v", err)
	}

	resolver.Images["docker.io/library/nginx:1.27"].Descriptor.Digest = "sha256:9999999999999999999999999999999999999999999999999999999999999999"

	second, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace: "default",
			Image:     &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"},
		},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(second mutable image) error = %v", err)
	}
	if first.GetEnvironment().GetID() == second.GetEnvironment().GetID() {
		t.Fatalf("mutable image returned same environment id %q after digest changed", first.GetEnvironment().GetID())
	}
	if first.GetEnvironment().GetSpecHash() == second.GetEnvironment().GetSpecHash() {
		t.Fatalf("mutable image returned same spec hash %q after digest changed", first.GetEnvironment().GetSpecHash())
	}
}

func TestFunctionDeployCreatesReadableFunction(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	req := uploadedFunctionDeployRequest(t, public, "hello", []byte("function bundle v1"))
	deployed, err := public.DeployFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("DeployFunction() error = %v", err)
	}
	if deployed.GetFunction().GetName() != "hello" || deployed.GetFunction().GetNamespace() != "default" {
		t.Fatalf("function identity = %s/%s", deployed.GetFunction().GetNamespace(), deployed.GetFunction().GetName())
	}
	if deployed.GetRevision().GetRevisionNumber() != 1 {
		t.Fatalf("revision number = %d, want 1", deployed.GetRevision().GetRevisionNumber())
	}
	if deployed.GetFunction().GetActiveRevisionID() != deployed.GetRevision().GetID() {
		t.Fatalf("active revision = %q, want %q", deployed.GetFunction().GetActiveRevisionID(), deployed.GetRevision().GetID())
	}
	if deployed.GetFunction().GetStatus() != functionv1.FunctionStatus_FUNCTION_STATUS_READY {
		t.Fatalf("function status = %s, want READY", deployed.GetFunction().GetStatus())
	}
	if deployed.GetDeployment().GetStatus() != functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO {
		t.Fatalf("deployment status = %s, want SCALED_TO_ZERO", deployed.GetDeployment().GetStatus())
	}
	if deployed.GetDeployment().GetWorkerServiceID() == "" {
		t.Fatal("deployment worker_service_id is empty")
	}
	worker, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: deployed.GetDeployment().GetWorkerServiceID()})
	if err != nil {
		t.Fatalf("GetService(worker) error = %v", err)
	}
	if worker.GetService().GetLabels()["axern.io/function-id"] != deployed.GetFunction().GetID() {
		t.Fatalf("worker function label = %q, want %q", worker.GetService().GetLabels()["axern.io/function-id"], deployed.GetFunction().GetID())
	}
	if worker.GetService().GetReplicas() != 0 {
		t.Fatalf("worker replicas = %d, want 0", worker.GetService().GetReplicas())
	}
	if got := worker.GetService().GetConfig().GetEnv()["AXERN_FUNCTION_HANDLER"]; got != "handler.hello" {
		t.Fatalf("worker handler env = %q, want handler.hello", got)
	}

	byID, err := public.GetFunction(context.Background(), &functionv1.GetFunctionRequest{FunctionID: deployed.GetFunction().GetID()})
	if err != nil {
		t.Fatalf("GetFunction(id) error = %v", err)
	}
	if byID.GetActiveRevision().GetID() != deployed.GetRevision().GetID() {
		t.Fatalf("active revision by id = %q, want %q", byID.GetActiveRevision().GetID(), deployed.GetRevision().GetID())
	}

	byName, err := public.GetFunction(context.Background(), &functionv1.GetFunctionRequest{Name: "hello"})
	if err != nil {
		t.Fatalf("GetFunction(name) error = %v", err)
	}
	if byName.GetFunction().GetID() != deployed.GetFunction().GetID() {
		t.Fatalf("function id by name = %q, want %q", byName.GetFunction().GetID(), deployed.GetFunction().GetID())
	}

	list, err := public.ListFunctions(context.Background(), &functionv1.ListFunctionsRequest{
		Filter: &functionv1.FunctionListFilter{Namespace: "default", Labels: map[string]string{"team": "runtime"}},
	})
	if err != nil {
		t.Fatalf("ListFunctions() error = %v", err)
	}
	if len(list.GetFunctions()) != 1 || list.GetFunctions()[0].GetID() != deployed.GetFunction().GetID() {
		t.Fatalf("listed functions = %+v", list.GetFunctions())
	}

	events, err := public.ListFunctionEvents(context.Background(), &functionv1.ListFunctionEventsRequest{FunctionID: deployed.GetFunction().GetID()})
	if err != nil {
		t.Fatalf("ListFunctionEvents() error = %v", err)
	}
	if len(events.GetEvents()) != 3 {
		t.Fatalf("event count = %d, want 3", len(events.GetEvents()))
	}

	repeated, err := public.DeployFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("DeployFunction(repeated) error = %v", err)
	}
	if repeated.GetRevision().GetID() != deployed.GetRevision().GetID() {
		t.Fatalf("repeated deploy revision = %q, want %q", repeated.GetRevision().GetID(), deployed.GetRevision().GetID())
	}

	changed, err := public.DeployFunction(context.Background(), uploadedFunctionDeployRequest(t, public, "hello", []byte("function bundle v2")))
	if err != nil {
		t.Fatalf("DeployFunction(changed) error = %v", err)
	}
	if changed.GetRevision().GetRevisionNumber() != 2 {
		t.Fatalf("changed revision number = %d, want 2", changed.GetRevision().GetRevisionNumber())
	}
	if changed.GetRevision().GetID() == deployed.GetRevision().GetID() {
		t.Fatal("changed deploy reused previous revision id")
	}
	if changed.GetDeployment().GetWorkerServiceID() != deployed.GetDeployment().GetWorkerServiceID() {
		t.Fatalf("changed deploy worker service = %q, want %q", changed.GetDeployment().GetWorkerServiceID(), deployed.GetDeployment().GetWorkerServiceID())
	}
}

func TestFunctionBundleUploadFeedsDeploySource(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	payload := []byte("function bundle")
	sum := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	stream := &functionBundleUploadStream{
		ctx: context.Background(),
		requests: []*functionv1.UploadFunctionBundleRequest{
			{
				Request: &functionv1.UploadFunctionBundleRequest_Open{
					Open: &functionv1.UploadFunctionBundleOpen{
						Namespace: "default",
						Name:      "hello",
						Digest:    digest,
						MediaType: "application/vnd.axern.function.tar",
						SizeBytes: int64(len(payload)),
					},
				},
			},
			{Request: &functionv1.UploadFunctionBundleRequest_Chunk{Chunk: payload}},
		},
	}
	if err := public.UploadFunctionBundle(stream); err != nil {
		t.Fatalf("UploadFunctionBundle() error = %v", err)
	}
	if stream.response.GetBundle().GetDigest() != digest {
		t.Fatalf("uploaded digest = %q, want %q", stream.response.GetBundle().GetDigest(), digest)
	}
	if stream.response.GetBundle().GetStorageUri() == "" {
		t.Fatal("uploaded storage_uri is empty")
	}
	repeatedWithDifferentMediaType := &functionBundleUploadStream{
		ctx: context.Background(),
		requests: []*functionv1.UploadFunctionBundleRequest{
			{
				Request: &functionv1.UploadFunctionBundleRequest_Open{
					Open: &functionv1.UploadFunctionBundleOpen{
						Namespace: "default",
						Name:      "hello",
						Digest:    digest,
						MediaType: "application/octet-stream",
						SizeBytes: int64(len(payload)),
					},
				},
			},
			{Request: &functionv1.UploadFunctionBundleRequest_Chunk{Chunk: payload}},
		},
	}
	if err := public.UploadFunctionBundle(repeatedWithDifferentMediaType); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("UploadFunctionBundle(repeated media mismatch) code = %v, want %v", grpcstatus.Code(err), codes.FailedPrecondition)
	}

	req := validFunctionDeployRequest("hello", digest)
	req.GetSource().GetBundle().StorageUri = stream.response.GetBundle().GetStorageUri()
	req.GetSource().GetBundle().SizeBytes = stream.response.GetBundle().GetSizeBytes()
	deployed, err := public.DeployFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("DeployFunction(uploaded bundle) error = %v", err)
	}
	if deployed.GetRevision().GetSource().GetBundle().GetStorageUri() != stream.response.GetBundle().GetStorageUri() {
		t.Fatalf("revision storage_uri = %q, want %q", deployed.GetRevision().GetSource().GetBundle().GetStorageUri(), stream.response.GetBundle().GetStorageUri())
	}
	if deployed.GetDeployment().GetWorkerServiceID() == "" {
		t.Fatal("uploaded bundle deploy worker_service_id is empty")
	}

	badSize := validFunctionDeployRequest("bad-size", digest)
	badSize.GetSource().GetBundle().StorageUri = stream.response.GetBundle().GetStorageUri()
	badSize.GetSource().GetBundle().SizeBytes = stream.response.GetBundle().GetSizeBytes() + 1
	if _, err := public.DeployFunction(context.Background(), badSize); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DeployFunction(size mismatch) code = %v, want %v", grpcstatus.Code(err), codes.FailedPrecondition)
	}
}

func TestFunctionDeployRequiresUploadedControlPlaneBundle(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	payload := []byte("missing function bundle")
	sum := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	req := validFunctionDeployRequest("hello", digest)
	req.GetSource().GetBundle().StorageUri = "axern://function-bundles/" + digest + ".tar"

	if _, err := public.DeployFunction(context.Background(), req); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DeployFunction() code = %v, want %v", grpcstatus.Code(err), codes.FailedPrecondition)
	}
}

func TestFunctionDeployValidation(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()
	validDigest := "sha256:" + strings.Repeat("1", 64)

	tests := []struct {
		name string
		req  *functionv1.DeployFunctionRequest
		code codes.Code
	}{
		{
			name: "wait ready is unimplemented",
			req: func() *functionv1.DeployFunctionRequest {
				req := validFunctionDeployRequest("hello", validDigest)
				req.WaitReady = true
				return req
			}(),
			code: codes.Unimplemented,
		},
		{
			name: "name must be stable ascii",
			req: func() *functionv1.DeployFunctionRequest {
				req := validFunctionDeployRequest("-hello", validDigest)
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "bundle digest is required",
			req: func() *functionv1.DeployFunctionRequest {
				req := validFunctionDeployRequest("hello", "")
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "bundle storage uri is required",
			req: func() *functionv1.DeployFunctionRequest {
				req := validFunctionDeployRequest("hello", validDigest)
				req.GetSource().GetBundle().StorageUri = ""
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "image source is not part of function deploy",
			req: func() *functionv1.DeployFunctionRequest {
				req := validFunctionDeployRequest("hello", validDigest)
				req.Source = &functionv1.FunctionSource{Source: &functionv1.FunctionSource_Image{Image: &functionv1.FunctionImageSource{Ref: "example.com/function:latest"}}}
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "scaling max cannot be below min",
			req: func() *functionv1.DeployFunctionRequest {
				req := validFunctionDeployRequest("hello", validDigest)
				req.GetSpec().GetScaling().MinReplicas = 2
				req.GetSpec().GetScaling().MaxReplicas = 1
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "scaling max must be positive",
			req: func() *functionv1.DeployFunctionRequest {
				req := validFunctionDeployRequest("hello", validDigest)
				req.GetSpec().GetScaling().MaxReplicas = 0
				return req
			}(),
			code: codes.InvalidArgument,
		},
		{
			name: "argv is worker owned",
			req: func() *functionv1.DeployFunctionRequest {
				req := validFunctionDeployRequest("hello", validDigest)
				req.GetSpec().GetConfig().Argv = []string{"python", "handler.py"}
				return req
			}(),
			code: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := public.DeployFunction(context.Background(), tt.req)
			if grpcstatus.Code(err) != tt.code {
				t.Fatalf("DeployFunction() code = %v, want %v", grpcstatus.Code(err), tt.code)
			}
		})
	}
}

func TestListFunctionEventsRequiresFilter(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	if _, err := public.ListFunctionEvents(context.Background(), &functionv1.ListFunctionEventsRequest{}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListFunctionEvents() code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
}

func TestFunctionListPagination(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	for _, name := range []string{"hello-a", "hello-b", "hello-c"} {
		if _, err := public.DeployFunction(context.Background(), uploadedFunctionDeployRequest(t, public, name, []byte(name+" bundle"))); err != nil {
			t.Fatalf("DeployFunction(%s) error = %v", name, err)
		}
	}

	first, err := public.ListFunctions(context.Background(), &functionv1.ListFunctionsRequest{
		Filter: &functionv1.FunctionListFilter{Namespace: "default", PageSize: 2},
	})
	if err != nil {
		t.Fatalf("ListFunctions(first) error = %v", err)
	}
	if len(first.GetFunctions()) != 2 {
		t.Fatalf("first page size = %d, want 2", len(first.GetFunctions()))
	}
	if first.GetNextCursor() == "" {
		t.Fatal("first page next_cursor is empty")
	}

	second, err := public.ListFunctions(context.Background(), &functionv1.ListFunctionsRequest{
		Filter: &functionv1.FunctionListFilter{Namespace: "default", PageSize: 2, Cursor: first.GetNextCursor()},
	})
	if err != nil {
		t.Fatalf("ListFunctions(second) error = %v", err)
	}
	if len(second.GetFunctions()) != 1 {
		t.Fatalf("second page size = %d, want 1", len(second.GetFunctions()))
	}
	if second.GetNextCursor() != "" {
		t.Fatalf("second page next_cursor = %q, want empty", second.GetNextCursor())
	}
	if first.GetFunctions()[0].GetID() == second.GetFunctions()[0].GetID() || first.GetFunctions()[1].GetID() == second.GetFunctions()[0].GetID() {
		t.Fatal("second page repeated a function from first page")
	}
}

func TestFunctionInvokeRecordsFailure(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	deployed, err := public.DeployFunction(context.Background(), uploadedFunctionDeployRequest(t, public, "hello", []byte("function bundle")))
	if err != nil {
		t.Fatalf("DeployFunction() error = %v", err)
	}

	invoked, err := public.InvokeFunction(context.Background(), &functionv1.InvokeFunctionRequest{
		Name:      "hello",
		Mode:      functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_SYNC,
		Payload:   &functionv1.FunctionPayload{ContentType: "application/json", Data: []byte(`{"name":"Axern"}`)},
		RequestID: "req-1",
		Labels:    map[string]string{"request": "test"},
	})
	if err != nil {
		t.Fatalf("InvokeFunction() error = %v", err)
	}
	if invoked.GetInvocation().GetStatus() != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_FAILED {
		t.Fatalf("invocation status = %s, want FAILED", invoked.GetInvocation().GetStatus())
	}
	if invoked.GetInvocation().GetError().GetCode() != "worker_not_ready" {
		t.Fatalf("invocation error code = %q, want worker_not_ready", invoked.GetInvocation().GetError().GetCode())
	}
	if invoked.GetInvocation().GetFunctionID() != deployed.GetFunction().GetID() {
		t.Fatalf("invocation function id = %q, want %q", invoked.GetInvocation().GetFunctionID(), deployed.GetFunction().GetID())
	}

	got, err := public.GetFunctionInvocation(context.Background(), &functionv1.GetFunctionInvocationRequest{InvocationID: invoked.GetInvocation().GetID()})
	if err != nil {
		t.Fatalf("GetFunctionInvocation() error = %v", err)
	}
	if got.GetInvocation().GetRequestID() != "req-1" {
		t.Fatalf("invocation request_id = %q, want req-1", got.GetInvocation().GetRequestID())
	}

	list, err := public.ListFunctionInvocations(context.Background(), &functionv1.ListFunctionInvocationsRequest{
		Filter: &functionv1.FunctionInvocationListFilter{FunctionID: deployed.GetFunction().GetID(), PageSize: 1},
	})
	if err != nil {
		t.Fatalf("ListFunctionInvocations() error = %v", err)
	}
	if len(list.GetInvocations()) != 1 || list.GetInvocations()[0].GetID() != invoked.GetInvocation().GetID() {
		t.Fatalf("listed invocations = %+v", list.GetInvocations())
	}

	events, err := public.ListFunctionEvents(context.Background(), &functionv1.ListFunctionEventsRequest{InvocationID: invoked.GetInvocation().GetID()})
	if err != nil {
		t.Fatalf("ListFunctionEvents(invocation) error = %v", err)
	}
	if len(events.GetEvents()) != 2 {
		t.Fatalf("invocation event count = %d, want 2", len(events.GetEvents()))
	}
	if events.GetEvents()[0].GetType() != functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_INVOCATION_FAILED {
		t.Fatalf("latest invocation event = %s, want INVOCATION_FAILED", events.GetEvents()[0].GetType())
	}
}

func TestFunctionDeleteSoftDeletesAndRejectsInvoke(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	deployed, err := public.DeployFunction(context.Background(), uploadedFunctionDeployRequest(t, public, "hello", []byte("function bundle")))
	if err != nil {
		t.Fatalf("DeployFunction() error = %v", err)
	}

	deleted, err := public.DeleteFunction(context.Background(), &functionv1.DeleteFunctionRequest{Name: "hello"})
	if err != nil {
		t.Fatalf("DeleteFunction() error = %v", err)
	}
	if deleted.GetFunction().GetID() != deployed.GetFunction().GetID() {
		t.Fatalf("deleted function id = %q, want %q", deleted.GetFunction().GetID(), deployed.GetFunction().GetID())
	}
	if deleted.GetFunction().GetStatus() != functionv1.FunctionStatus_FUNCTION_STATUS_DELETED {
		t.Fatalf("deleted function status = %s, want DELETED", deleted.GetFunction().GetStatus())
	}
	if deleted.GetFunction().GetDeploymentStatus() != functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO {
		t.Fatalf("deleted deployment status = %s, want SCALED_TO_ZERO", deleted.GetFunction().GetDeploymentStatus())
	}
	workerAfterDelete, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: deployed.GetDeployment().GetWorkerServiceID()})
	if err != nil {
		t.Fatalf("GetService(worker after delete) error = %v", err)
	}
	if workerAfterDelete.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		t.Fatalf("worker service status after delete = %s, want DELETED", workerAfterDelete.GetService().GetStatus())
	}

	byName, err := public.GetFunction(context.Background(), &functionv1.GetFunctionRequest{Name: "hello"})
	if err != nil {
		t.Fatalf("GetFunction(deleted) error = %v", err)
	}
	if byName.GetFunction().GetStatus() != functionv1.FunctionStatus_FUNCTION_STATUS_DELETED {
		t.Fatalf("GetFunction(deleted) status = %s, want DELETED", byName.GetFunction().GetStatus())
	}
	if byName.GetDeployment().GetStatus() != functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO {
		t.Fatalf("GetFunction(deleted) deployment status = %s, want SCALED_TO_ZERO", byName.GetDeployment().GetStatus())
	}

	events, err := public.ListFunctionEvents(context.Background(), &functionv1.ListFunctionEventsRequest{FunctionID: deployed.GetFunction().GetID()})
	if err != nil {
		t.Fatalf("ListFunctionEvents(deleted) error = %v", err)
	}
	if len(events.GetEvents()) != 4 {
		t.Fatalf("event count after delete = %d, want 4", len(events.GetEvents()))
	}
	if events.GetEvents()[0].GetType() != functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_CLEANUP {
		t.Fatalf("latest event type = %s, want CLEANUP", events.GetEvents()[0].GetType())
	}

	if _, err := public.DeleteFunction(context.Background(), &functionv1.DeleteFunctionRequest{Name: "hello"}); err != nil {
		t.Fatalf("DeleteFunction(repeated) error = %v", err)
	}
	if _, err := public.InvokeFunction(context.Background(), &functionv1.InvokeFunctionRequest{Name: "hello", Mode: functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_SYNC}); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("InvokeFunction() code = %v, want %v", grpcstatus.Code(err), codes.FailedPrecondition)
	}
}

func validFunctionDeployRequest(name, digest string) *functionv1.DeployFunctionRequest {
	return &functionv1.DeployFunctionRequest{
		Name: name,
		Spec: &functionv1.FunctionSpec{
			Runtime: "python3.11",
			Handler: "handler.hello",
			WorkerSource: &functionv1.FunctionWorkerSource{Source: &functionv1.FunctionWorkerSource_Environment{
				Environment: &environmentv1.EnvironmentSpec{TemplateID: "python311"},
			}},
			Config: &commonv1.ExecutionConfig{
				Env: map[string]string{"GREETING": "hello"},
			},
			Scaling: &functionv1.FunctionScalingSpec{MinReplicas: 0, MaxReplicas: 1, Concurrency: 1},
		},
		Source: &functionv1.FunctionSource{
			Source: &functionv1.FunctionSource_Bundle{
				Bundle: &functionv1.FunctionBundleSource{
					Digest:     digest,
					MediaType:  "application/vnd.axern.function.tar",
					SizeBytes:  128,
					StorageUri: "axern://function-bundles/" + strings.TrimPrefix(digest, "sha256:") + ".tar",
				},
			},
		},
		Labels: map[string]string{"team": "runtime"},
	}
}

func uploadedFunctionDeployRequest(t *testing.T, public *publicv1.Server, name string, payload []byte) *functionv1.DeployFunctionRequest {
	t.Helper()
	bundle := uploadFunctionBundle(t, public, name, payload)
	req := validFunctionDeployRequest(name, bundle.GetDigest())
	req.GetSource().GetBundle().StorageUri = bundle.GetStorageUri()
	req.GetSource().GetBundle().SizeBytes = bundle.GetSizeBytes()
	req.GetSource().GetBundle().MediaType = bundle.GetMediaType()
	return req
}

func uploadFunctionBundle(t *testing.T, public *publicv1.Server, name string, payload []byte) *functionv1.FunctionBundleSource {
	t.Helper()
	sum := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	stream := &functionBundleUploadStream{
		ctx: context.Background(),
		requests: []*functionv1.UploadFunctionBundleRequest{
			{
				Request: &functionv1.UploadFunctionBundleRequest_Open{
					Open: &functionv1.UploadFunctionBundleOpen{
						Namespace: "default",
						Name:      name,
						Digest:    digest,
						MediaType: "application/vnd.axern.function.tar",
						SizeBytes: int64(len(payload)),
					},
				},
			},
			{Request: &functionv1.UploadFunctionBundleRequest_Chunk{Chunk: payload}},
		},
	}
	if err := public.UploadFunctionBundle(stream); err != nil {
		t.Fatalf("UploadFunctionBundle() error = %v", err)
	}
	return stream.response.GetBundle()
}

type functionBundleUploadStream struct {
	ctx      context.Context
	requests []*functionv1.UploadFunctionBundleRequest
	response *functionv1.UploadFunctionBundleResponse
	index    int
}

func (s *functionBundleUploadStream) SendAndClose(response *functionv1.UploadFunctionBundleResponse) error {
	s.response = response
	return nil
}

func (s *functionBundleUploadStream) Recv() (*functionv1.UploadFunctionBundleRequest, error) {
	if s.index >= len(s.requests) {
		return nil, io.EOF
	}
	req := s.requests[s.index]
	s.index++
	return req, nil
}

func (s *functionBundleUploadStream) SetHeader(metadata.MD) error  { return nil }
func (s *functionBundleUploadStream) SendHeader(metadata.MD) error { return nil }
func (s *functionBundleUploadStream) SetTrailer(metadata.MD)       {}
func (s *functionBundleUploadStream) Context() context.Context     { return s.ctx }
func (s *functionBundleUploadStream) SendMsg(any) error            { return nil }
func (s *functionBundleUploadStream) RecvMsg(any) error            { return nil }

func TestRunLeaseAndAllocationStatusFlow(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	now := time.Now().UTC()
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	if _, err := node.RegisterNode(context.Background(), &nodev1.RegisterNodeRequest{NodeID: "node-a", Runtimes: []string{"runsc"}, NodeTarget: "127.0.0.1:25000", NodeAuthToken: "test-node-token"}); err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}
	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{NodeID: "node-a", Runtimes: []string{"runsc"}, NodeTarget: "127.0.0.1:25000", NodeAuthToken: "test-node-token", Summary: controldtest.ReadySummary(now)}); err != nil {
		t.Fatalf("ReportNode() error = %v", err)
	}
	envResp, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{TemplateID: "python311", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment() error = %v", err)
	}
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: envResp.GetEnvironment().GetID(),
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/true"}},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: runResp.GetRun().GetAllocationID(),
			Attempt:      runResp.GetRun().GetAttempt(),
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
			ExitCode:     0,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus() error = %v", err)
	}
	got, err := public.GetRun(context.Background(), &runv1.GetRunRequest{RunID: runResp.GetRun().GetID()})
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.GetRun().GetStatus() != runv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("run status = %s, want SUCCEEDED", got.GetRun().GetStatus())
	}
}

func TestRunAllowsImageDefaultArgv(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	envResp, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{TemplateID: "python311", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment() error = %v", err)
	}
	_, err = public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: envResp.GetEnvironment().GetID(),
		Config:        &commonv1.ExecutionConfig{},
	})
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CreateRun(empty argv) code = %v, want FailedPrecondition from placement after argv validation passes (err=%v)", grpcstatus.Code(err), err)
	}
}

func TestServiceReplicaFallbackViewIsEmptyWithoutAuthoritativeAllocations(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	envResp, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{TemplateID: "python311", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment() error = %v", err)
	}
	serviceResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: envResp.GetEnvironment().GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	listResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceResp.GetService().GetID(),
		Filter:    &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT},
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas() error = %v", err)
	}
	if len(listResp.GetReplicas()) != 0 {
		t.Fatalf("fallback replicas = %d, want 0", len(listResp.GetReplicas()))
	}
	_, err = public.GetServiceReplica(context.Background(), &servicev1.GetServiceReplicaRequest{
		ServiceID: serviceResp.GetService().GetID(),
		ReplicaID: "alloc-missing",
	})
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("GetServiceReplica() code = %v, want %v", grpcstatus.Code(err), codes.NotFound)
	}
}

func newTestService(t *testing.T) *app.App {
	t.Helper()
	return newTestServiceWithImageResolver(t, controldtest.NewFakeImageResolver())
}

func newTestServiceWithImageResolver(t *testing.T, resolver *controldtest.FakeImageResolver) *app.App {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	controldtest.ResetPostgresControlTables(t, dsn)
	service, err := app.New(app.Config{
		PostgresDSN:      dsn,
		SecretsMasterKey: "test-only-master-key-32-bytes!!!",
		TunnelRelays:     "test,127.0.0.1:24210,tunneld:24210,1,false",
		NodeLifecycle:    &controldtest.FakeNodeLifecycleClient{},
		ImageResolver:    resolver,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return service
}
