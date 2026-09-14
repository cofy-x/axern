package app

import (
	"context"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestPostgresAdminRetiresIdleNodeAndFencesReporter(t *testing.T) {
	app, _ := newPostgresTestServiceWithConfig(t, Config{HeartbeatFreshnessWindow: time.Hour, SummaryFreshnessWindow: time.Hour})
	defer app.Close()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	registerReadyNode(t, app, "node-a", now)
	app.now = func() time.Time { return now.Add(2 * time.Hour) }

	resp, err := app.AdminV1Handler().RetireAdminNode(context.Background(), &adminv1.RetireAdminNodeRequest{NodeID: "node-a", OperatorReason: "host permanently removed"})
	if err != nil {
		t.Fatalf("RetireAdminNode() error = %v", err)
	}
	if resp.GetNode().GetLifecycleStatus() != adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_RETIRED || resp.GetNode().GetRetiredReason() != "host permanently removed" {
		t.Fatalf("retired node = %+v", resp.GetNode())
	}

	list, err := app.AdminV1Handler().ListAdminNodes(context.Background(), &adminv1.ListAdminNodesRequest{LifecycleStatus: adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_RETIRED})
	if err != nil || len(list.GetNodes()) != 1 {
		t.Fatalf("ListAdminNodes() = %+v, %v", list, err)
	}
	audit, err := app.AdminV1Handler().ListAdminAuditEvents(context.Background(), &adminv1.ListAdminAuditEventsRequest{Filter: &adminv1.AdminAuditEventFilter{Operation: adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_RETIRE_NODE, TargetType: adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_NODE, TargetID: "node-a"}})
	if err != nil || len(audit.GetEvents()) != 1 {
		t.Fatalf("ListAdminAuditEvents() = %+v, %v", audit, err)
	}
	_, err = app.NodeV1Handler().ReportNode(context.Background(), &nodev1.ReportNodeRequest{NodeID: "node-a", NodeAuthToken: "test-node-token", Summary: controldtest.ReadySummary(now.Add(2 * time.Hour))})
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ReportNode(retired) error = %v", err)
	}
}

func TestPostgresAdminRejectsRetiringNodeWithActiveAllocation(t *testing.T) {
	app, _ := newPostgresTestServiceWithConfig(t, Config{HeartbeatFreshnessWindow: time.Hour, SummaryFreshnessWindow: time.Hour})
	defer app.Close()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	registerReadyNode(t, app, "node-a", now)
	app.now = func() time.Time { return now.Add(2 * time.Hour) }
	insertAdminNodeTestRun(t, app, "run-a", now)
	if _, err := app.db.Pool().Exec(context.Background(), `INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, created_at, updated_at) VALUES ('alloc-a', 'run-a', 'node-a', 'ALLOCATION_LIFECYCLE_STATE_ACTIVE', $1, $1)`, now); err != nil {
		t.Fatalf("insert active allocation: %v", err)
	}
	_, err := app.AdminV1Handler().RetireAdminNode(context.Background(), &adminv1.RetireAdminNodeRequest{NodeID: "node-a", OperatorReason: "host permanently removed"})
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("RetireAdminNode(active allocation) error = %v", err)
	}
}

func TestPostgresAdminRetiresNodeWithReleasedAllocation(t *testing.T) {
	app, _ := newPostgresTestServiceWithConfig(t, Config{HeartbeatFreshnessWindow: time.Hour, SummaryFreshnessWindow: time.Hour})
	defer app.Close()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	registerReadyNode(t, app, "node-a", now)
	app.now = func() time.Time { return now.Add(2 * time.Hour) }
	insertAdminNodeTestRun(t, app, "run-a", now)
	if _, err := app.db.Pool().Exec(context.Background(), `INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, created_at, updated_at) VALUES ('alloc-a', 'run-a', 'node-a', 'ALLOCATION_LIFECYCLE_STATE_RELEASED', $1, $1)`, now); err != nil {
		t.Fatalf("insert released allocation: %v", err)
	}
	if _, err := app.db.Pool().Exec(context.Background(), `UPDATE runs SET status = 'RUN_STATUS_FAILED' WHERE run_id = 'run-a'`); err != nil {
		t.Fatalf("mark historical run terminal: %v", err)
	}
	if _, err := app.AdminV1Handler().RetireAdminNode(context.Background(), &adminv1.RetireAdminNodeRequest{NodeID: "node-a", OperatorReason: "host permanently removed"}); err != nil {
		t.Fatalf("RetireAdminNode() error = %v", err)
	}
}

func insertAdminNodeTestRun(t *testing.T, app *App, runID string, now time.Time) {
	t.Helper()
	if _, err := app.db.Pool().Exec(context.Background(), `
		INSERT INTO namespaces (namespace, created_at)
		VALUES ('default', $1)
		ON CONFLICT (namespace) DO NOTHING
	`, now); err != nil {
		t.Fatalf("insert namespace: %v", err)
	}
	if _, err := app.db.Pool().Exec(context.Background(), `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, created_at, updated_at)
		VALUES ($1, 'default', 'env-test', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, $2, $2)
	`, runID, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
}
