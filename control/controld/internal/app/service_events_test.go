package app

import (
	"context"
	"errors"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestPostgresServiceEventsExplainInitialCreatePlacementFailure(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 18, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	observed := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	if observed.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("reconciled status = %v, want DEGRADED when no placement candidates exist", observed.GetStatus())
	}
	if observed.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_NODE_SELECTION_ERROR {
		t.Fatalf("reconciled diagnostic_code = %v, want NODE_SELECTION_ERROR", observed.GetDiagnosticCode())
	}

	eventsResp, err := public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: createResp.GetService().GetID(),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents() error = %v", err)
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED); got != 1 {
		t.Fatalf("SERVICE_DEGRADED count = %d, want 1", got)
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED); got != 0 {
		t.Fatalf("REPLACEMENT_BLOCKED count = %d, want 0 for non-rollout placement failure", got)
	}
	event := findFirstServiceEvent(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED)
	if event == nil {
		t.Fatal("missing SERVICE_DEGRADED event")
	}
	if event.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_NODE_SELECTION_ERROR {
		t.Fatalf("SERVICE_DEGRADED diagnostic = %v, want NODE_SELECTION_ERROR", event.GetDiagnosticCode())
	}
	if event.GetMessage() == "" {
		t.Fatal("SERVICE_DEGRADED message = empty, want placement failure context")
	}
}

func TestPostgresServiceInitialCreateLifecycleRetryIsObservable(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 18, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	lifecycle.CreateErr = errors.New("runtime create failed: container start timeout")

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	observed := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	if observed.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("reconciled status = %v, want RECONCILING while lifecycle create retries", observed.GetStatus())
	}
	if observed.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR {
		t.Fatalf("reconciled diagnostic_code = %v, want RUNTIME_START_ERROR", observed.GetDiagnosticCode())
	}

	eventsResp, err := public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: createResp.GetService().GetID(),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents() error = %v", err)
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED); got != 0 {
		t.Fatalf("SERVICE_DEGRADED count = %d, want 0 while retry remains active", got)
	}
	replicas, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("ListServiceReplicas() error = %v", err)
	}
	if len(replicas.GetReplicas()) != 1 || replicas.GetReplicas()[0].GetLifecycleRetry() == nil {
		t.Fatalf("replicas = %+v, want one replica with lifecycle retry", replicas.GetReplicas())
	}
	if replicas.GetReplicas()[0].GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR {
		t.Fatalf("replica diagnostic = %v, want RUNTIME_START_ERROR", replicas.GetReplicas()[0].GetDiagnosticCode())
	}
}

func TestPostgresServiceCreateExplainsReadonlyRootfsMountTargetFailure(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 18, 45, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	lifecycle.CreateErr = errors.New(`rpc error: code = Internal desc = node start failed: Failed to validate mount targets: mount target "/var/lib/app" does not exist in readonly rootfs`)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
		Config: &commonv1.ExecutionConfig{
			VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name:   "data",
				Target: "/var/lib/app",
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	observed := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	wantMessage := `mount target "/var/lib/app" does not exist in the readonly image rootfs; use an existing image path or disable readonly rootfs`
	if got := observed.GetMessage(); got != wantMessage {
		t.Fatalf("reconciled service message = %q, want %q", got, wantMessage)
	}

	replicasResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("ListServiceReplicas() error = %v", err)
	}
	if len(replicasResp.GetReplicas()) != 1 || replicasResp.GetReplicas()[0].GetLifecycleRetry() == nil {
		t.Fatalf("replicas = %+v, want one replica with lifecycle retry", replicasResp.GetReplicas())
	}
	if got := replicasResp.GetReplicas()[0].GetDiagnosticCode(); got != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR {
		t.Fatalf("replica diagnostic = %v, want RUNTIME_START_ERROR", got)
	}
}
