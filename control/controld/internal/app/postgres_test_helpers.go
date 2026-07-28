package app

import (
	"context"
	"os"
	"testing"
	"time"

	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	pgconsistency "github.com/cofy-x/axern/control/controld/internal/postgres/consistency"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func newPostgresTestService(t *testing.T) (*App, *controldtest.FakeNodeLifecycleClient) {
	t.Helper()
	return newPostgresTestServiceWithConfig(t, Config{})
}

func newPostgresTestServiceWithConfig(t *testing.T, cfg Config) (*App, *controldtest.FakeNodeLifecycleClient) {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	controldtest.ResetPostgresControlTables(t, dsn)
	lifecycle := &controldtest.FakeNodeLifecycleClient{}
	cfg.PostgresDSN = dsn
	cfg.SecretsMasterKey = "test-only-master-key-32-bytes!!!"
	cfg.TunnelRelays = "test,127.0.0.1:24210,tunneld:24210,1,false"
	cfg.NodeLifecycle = lifecycle
	if cfg.ImageResolver == nil {
		cfg.ImageResolver = controldtest.NewFakeImageResolver()
	}
	app, err := newApp(cfg, false)
	if err != nil {
		t.Fatalf("New(postgres) error = %v", err)
	}
	app.registry.Replace(nil)
	return app, lifecycle
}

func registerReadyNode(t *testing.T, app *App, nodeID string, now time.Time) {
	t.Helper()
	node := app.NodeV1Handler()
	if _, err := node.RegisterNode(context.Background(), &nodev1.RegisterNodeRequest{
		NodeID:        nodeID,
		Runtimes:      []string{"runsc"},
		NodeTarget:    "127.0.0.1:25000",
		NodeAuthToken: "test-node-token",
	}); err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}
	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{
		NodeID:        nodeID,
		Runtimes:      []string{"runsc"},
		NodeTarget:    "127.0.0.1:25000",
		NodeAuthToken: "test-node-token",
		Summary:       controldtest.ReadySummary(now),
	}); err != nil {
		t.Fatalf("ReportNode() error = %v", err)
	}
}

func reconcileCreatedService(t *testing.T, app *App, serviceID string, now time.Time) *servicev1.Service {
	t.Helper()
	if err := app.serviceReconciler.ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending(service admission) error = %v", err)
	}
	response, err := app.PublicV1Handler().GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after admission) error = %v", err)
	}
	return response.GetService()
}

func createDefaultEnvironment(t *testing.T, app *App) *environmentv1.Environment {
	t.Helper()
	resp, err := app.PublicV1Handler().CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{TemplateID: "python311", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment() error = %v", err)
	}
	return resp.GetEnvironment()
}

func createImageEnvironment(t *testing.T, app *App, imageRef string) *environmentv1.Environment {
	t.Helper()
	resp, err := app.PublicV1Handler().CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace: "default",
			Image:     &environmentv1.EnvironmentImageSource{Ref: imageRef},
		},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(image) error = %v", err)
	}
	return resp.GetEnvironment()
}

func assertPostgresConsistencyOK(t *testing.T, app *App) {
	t.Helper()
	snapshot, err := pgconsistency.Snapshot(context.Background(), app.db.Pool(), app.now())
	if err != nil {
		t.Fatalf("consistency snapshot: %v", err)
	}
	if snapshot.Status != consistencykernel.StatusOK {
		t.Fatalf("consistency status = %q, want ok; issues=%+v", snapshot.Status, snapshot.Issues)
	}
}
