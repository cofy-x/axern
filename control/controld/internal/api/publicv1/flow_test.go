package publicv1_test

import (
	"context"
	"os"
	"testing"
	"time"

	app "github.com/cofy-x/axern/control/controld/internal/app"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	if !proto.Equal(first.GetEnvironment().GetResolvedSpec(), second.GetEnvironment().GetResolvedSpec()) {
		t.Fatal("equivalent template sources produced different resolved specifications")
	}
}

func TestCreateRunRejectsDeletedEnvironment(t *testing.T) {
	service := newTestService(t)
	defer service.Close()
	public := service.PublicV1Handler()
	created, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{Spec: &environmentv1.EnvironmentSpec{TemplateID: "python311", Namespace: "default"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := public.DeleteEnvironment(context.Background(), &environmentv1.DeleteEnvironmentRequest{EnvironmentID: created.GetEnvironment().GetID()}); err != nil {
		t.Fatal(err)
	}
	_, err = public.CreateRun(context.Background(), &runv1.CreateRunRequest{EnvironmentID: created.GetEnvironment().GetID(), Config: &commonv1.ExecutionConfig{Argv: []string{"true"}}})
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CreateRun() code = %v, want FailedPrecondition", grpcstatus.Code(err))
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
	if !proto.Equal(first.GetEnvironment().GetResolvedSpec(), second.GetEnvironment().GetResolvedSpec()) {
		t.Fatal("equivalent image sources produced different resolved specifications")
	}
	if got := first.GetEnvironment().GetSpec().GetImage().GetDigest(); got == "" {
		t.Fatal("resolved image digest = empty, want resolved digest persisted")
	}
	if got := first.GetEnvironment().GetResolvedSpec().GetImageDescriptor().GetDigest(); got != first.GetEnvironment().GetSpec().GetImage().GetDigest() {
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
	if first.GetEnvironment().GetResolvedSpec().GetImageDescriptor().GetDigest() == second.GetEnvironment().GetResolvedSpec().GetImageDescriptor().GetDigest() {
		t.Fatal("mutable image retained the old resolved digest")
	}
}

func TestRunLeaseAndAllocationLifecycleStateFlow(t *testing.T) {
	app := newTestService(t)
	defer app.Close()
	now := time.Now().UTC()
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	if _, err := node.RegisterNode(context.Background(), &nodev1.RegisterNodeRequest{NodeID: "node-a", NodeTarget: "127.0.0.1:25000", NodeAuthToken: "test-node-token"}); err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}
	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{NodeID: "node-a", NodeTarget: "127.0.0.1:25000", NodeAuthToken: "test-node-token", Summary: controldtest.ReadySummary(now)}); err != nil {
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
	if _, err := node.BatchReportAllocationLifecycle(context.Background(), &nodev1.BatchReportAllocationLifecycleRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationLifecycleObservation{{
			AllocationID: runResp.GetRun().GetAllocationID(),
			State:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED,
			ExitCode:     func() *int32 { value := int32(0); return &value }(),
			ObservedAt:   timestamppb.New(now.Add(time.Second)),
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationLifecycle() error = %v", err)
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
