package app

import (
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/placement"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
)

func TestNewRequiresPostgresDSN(t *testing.T) {
	lifecycle := &closeTrackingLifecycleClient{FakeNodeLifecycleClient: &controldtest.FakeNodeLifecycleClient{}}
	_, err := New(Config{NodeLifecycle: lifecycle})
	if err == nil || !strings.Contains(err.Error(), "postgres dsn is required") {
		t.Fatalf("New() error = %v, want postgres dsn required", err)
	}
	if !lifecycle.closed {
		t.Fatal("New() did not close initialized dependencies after configuration failure")
	}
}

type closeTrackingLifecycleClient struct {
	*controldtest.FakeNodeLifecycleClient
	closed bool
}

func (c *closeTrackingLifecycleClient) Close() error {
	c.closed = true
	return nil
}

func TestAuthoritativeProfileBuildsCompleteAPIs(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()

	selector := placement.NewSelector(
		app.registry,
		app.placement,
		func() time.Time { return app.now() },
		defaultSandboxRuntime,
	)
	profile := app.authoritativeProfile(selector)
	if profile.public.environments == nil || profile.public.runs == nil || profile.public.services == nil || profile.public.functions == nil {
		t.Fatal("authoritative profile did not build a complete public API dependency set")
	}
	if profile.node.allocations == nil {
		t.Fatal("authoritative profile did not build node allocation control")
	}
	if profile.serviceReconciler == nil {
		t.Fatal("authoritative profile did not expose a service reconciler")
	}
	if app.nodeReconciler == nil {
		t.Fatal("New(authoritative) did not build a node availability reconciler")
	}
	if app.PublicV1Handler() == nil || app.NodeV1Handler() == nil {
		t.Fatal("New(authoritative) did not build gRPC handlers")
	}
	if app.AdminV1Handler() == nil {
		t.Fatal("New(authoritative) did not build admin gRPC handler")
	}
}
