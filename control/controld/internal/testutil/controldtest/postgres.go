package controldtest

import (
	"context"
	"testing"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
)

func ResetPostgresControlTables(t *testing.T, dsn string) {
	t.Helper()
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres test db: %v", err)
	}
	defer db.Close()
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatalf("apply postgres migrations: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		TRUNCATE TABLE
			admin_audit_events,
			function_events,
			function_invocations,
			function_deployments,
			function_revisions,
			function_idempotency_records,
			function_bundles,
			functions,
			namespace_quota_events,
			tunnel_session_events,
			tunnel_sessions,
			allocation_reconcile_queue,
			execution_leases,
			workload_reservations,
			allocations,
			service_events,
			runs,
			services,
			secrets,
			environments,
			namespace_resource_quotas,
			namespaces,
			node_runtime_sets,
			node_summaries,
			nodes
		CASCADE
	`); err != nil {
		t.Fatalf("truncate postgres test tables: %v", err)
	}
}
