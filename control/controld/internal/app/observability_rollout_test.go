package app

import (
	"context"
	"testing"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

func TestObserveRolloutWorkQueueReportsCountsAndAges(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()

	if _, err := app.db.Pool().Exec(context.Background(), `
		INSERT INTO namespaces(namespace,created_at,updated_at)
		VALUES('default',now(),now());
		INSERT INTO rollouts(rollout_id,namespace,status,spec,spec_hash,created_at)
		VALUES('rol-metrics','default','ROLLOUT_STATUS_QUEUED','{}','sha256:metrics',now());
		INSERT INTO rollout_work_items(work_id,kind,rollout_id,status,next_run_at,claim_owner,lease_token_hash,lease_expires_at,cancel_requested,created_at,updated_at) VALUES
			('wrk-metric-pending-due','WORK_KIND_PLAN','rol-metrics','PENDING',now()-interval '2 minutes','','',NULL,FALSE,now(),now()),
			('wrk-metric-pending-scheduled','WORK_KIND_PLAN','rol-metrics','PENDING',now()+interval '1 minute','','',NULL,FALSE,now(),now()),
			('wrk-metric-pending-cancelled','WORK_KIND_PLAN','rol-metrics','PENDING',now()-interval '5 minutes','','',NULL,TRUE,now(),now()),
			('wrk-metric-leased-active','WORK_KIND_PLAN','rol-metrics','LEASED',now(),'worker','active',now()+interval '1 minute',FALSE,now(),now()),
			('wrk-metric-leased-expired','WORK_KIND_PLAN','rol-metrics','LEASED',now(),'worker','expired',now()-interval '3 minutes',FALSE,now(),now()),
			('wrk-metric-leased-cancelled','WORK_KIND_PLAN','rol-metrics','LEASED',now(),'worker','cancelled',now()-interval '5 minutes',TRUE,now(),now())
	`); err != nil {
		t.Fatal(err)
	}

	counts := map[string]int64{}
	if err := app.observeRolloutWorkQueue(context.Background(), func(value int64, attrs ...attribute.KeyValue) {
		counts[metricState(attrs)] = value
	}); err != nil {
		t.Fatal(err)
	}
	for state, want := range map[string]int64{
		"pending_due":       1,
		"pending_scheduled": 1,
		"leased_active":     1,
		"leased_expired":    1,
	} {
		if got := counts[state]; got != want {
			t.Fatalf("rollout work queue %s = %d, want %d", state, got, want)
		}
	}

	ages := map[string]float64{}
	if err := app.observeRolloutWorkOldestDueAge(context.Background(), func(value float64, attrs ...attribute.KeyValue) {
		ages[metricState(attrs)] = value
	}); err != nil {
		t.Fatal(err)
	}
	if got := ages["pending_due"]; got < 120 || got >= 240 {
		t.Fatalf("pending due age = %f, want [120,240)", got)
	}
	if got := ages["leased_expired"]; got < 180 || got >= 240 {
		t.Fatalf("expired lease age = %f, want [180,240)", got)
	}
}

func metricState(attrs []attribute.KeyValue) string {
	for _, attr := range attrs {
		if string(attr.Key) == sdkobs.AttrState {
			return attr.Value.AsString()
		}
	}
	return ""
}
