package app

import (
	"context"
	"errors"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestPostgresServiceReplicaDiagnosticsForSecretProjectionFailure(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 16, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	originalReplicaID := admitted.GetAllocationIds()[0]
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: originalReplicaID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(original RUNNING) error = %v", err)
	}
	currentResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(before secret rollout) error = %v", err)
	}

	updateResp, err := public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:       createResp.GetService().GetID(),
		ExpectedVersion: currentResp.GetService().GetVersion(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/new"},
			SecretEnv: []*commonv1.SecretEnvVar{{
				Name:     "TOKEN",
				SecretID: "sec-missing",
				Key:      "token",
			}},
		},
		UpdateMask: updateMask("config"),
	})
	if err != nil {
		t.Fatalf("UpdateService(secret rollout) error = %v", err)
	}
	reconcileAllocationLifecycle(t, app, now)
	rollout := updateResp.GetService().GetRolloutStatus()
	if rollout == nil || !rollout.GetInProgress() {
		t.Fatalf("rollout status = %+v, want in-progress blocked rollout", rollout)
	}
	if rollout.GetPhase() != servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY {
		t.Fatalf("rollout phase = %v, want WAITING_FOR_UPDATED_READY while create retries", rollout.GetPhase())
	}
	retryingResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(secret retry) error = %v", err)
	}
	if retryingResp.GetService().GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR {
		t.Fatalf("service diagnostic = %v, want SECRET_PROJECTION_ERROR", retryingResp.GetService().GetDiagnosticCode())
	}

	replicasResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: createResp.GetService().GetID(),
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas() error = %v", err)
	}
	var sawRetriedReplacement, sawOutdatedCurrent bool
	for _, replica := range replicasResp.GetReplicas() {
		if replica.GetID() == originalReplicaID {
			sawOutdatedCurrent = replica.GetOutdated()
			continue
		}
		if replica.GetLifecycleRetry() != nil &&
			replica.GetDiagnosticCode() == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR {
			sawRetriedReplacement = true
		}
	}
	if !sawOutdatedCurrent {
		t.Fatal("original replica not marked outdated during blocked secret rollout")
	}
	if !sawRetriedReplacement {
		t.Fatal("retrying replacement replica missing secret projection diagnostic")
	}

	eventsResp, err := public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: createResp.GetService().GetID(),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents() error = %v", err)
	}
	var sawRolloutStarted, sawReplacementBlocked, sawServiceDegraded bool
	for _, event := range eventsResp.GetEvents() {
		switch event.GetType() {
		case servicev1.ServiceEventType_SERVICE_EVENT_TYPE_ROLLOUT_STARTED:
			sawRolloutStarted = true
		case servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED:
			if event.GetDiagnosticCode() == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR {
				sawReplacementBlocked = true
			}
		case servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED:
			sawServiceDegraded = true
		}
	}
	if !sawRolloutStarted || sawReplacementBlocked || sawServiceDegraded {
		t.Fatalf("service events do not match active retry semantics: %#v", eventsResp.GetEvents())
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_ROLLOUT_STARTED); got != 1 {
		t.Fatalf("ROLLOUT_STARTED count = %d, want 1", got)
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED); got != 0 {
		t.Fatalf("REPLACEMENT_BLOCKED count = %d, want 0 while retry remains active", got)
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED); got != 0 {
		t.Fatalf("SERVICE_DEGRADED count = %d, want 0 while retry remains active", got)
	}

	app.reconcileV1()

	eventsResp, err = public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: createResp.GetService().GetID(),
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents(after reconcile) error = %v", err)
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED); got != 0 {
		t.Fatalf("SERVICE_DEGRADED count after repeated reconcile = %d, want 0 while retry remains active", got)
	}
}

func TestPostgresServiceReplicaDiagnosticsForRegistryAuthFailure(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 17, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	initialEnv := createDefaultEnvironment(t, app)
	updatedEnv := createImageEnvironment(t, app, "ghcr.io/acme/private-app:v2")

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: initialEnv.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	originalReplicaID := admitted.GetAllocationIds()[0]
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: originalReplicaID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(original RUNNING) error = %v", err)
	}
	currentResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(before env rollout) error = %v", err)
	}

	lifecycle.CreateErr = errors.New("resolve image ref ghcr.io/acme/private-app:v2: authentication failed or access was denied; check the referenced docker-config-json secret and repository permissions")
	updateResp, err := public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:       createResp.GetService().GetID(),
		ExpectedVersion: currentResp.GetService().GetVersion(),
		EnvironmentID:   stringptr(updatedEnv.GetID()),
		UpdateMask:      updateMask("environment_id"),
	})
	if err != nil {
		t.Fatalf("UpdateService(environment_id) error = %v", err)
	}
	reconcileAllocationLifecycle(t, app, now)
	rollout := updateResp.GetService().GetRolloutStatus()
	if rollout == nil || !rollout.GetInProgress() {
		t.Fatalf("rollout status = %+v, want in-progress blocked rollout", rollout)
	}
	if rollout.GetPhase() != servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY {
		t.Fatalf("rollout phase = %v, want WAITING_FOR_UPDATED_READY while create retries", rollout.GetPhase())
	}
	retryingResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(registry retry) error = %v", err)
	}
	if retryingResp.GetService().GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR {
		t.Fatalf("service diagnostic = %v, want REGISTRY_AUTH_ERROR", retryingResp.GetService().GetDiagnosticCode())
	}

	replicasResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: createResp.GetService().GetID(),
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas() error = %v", err)
	}
	var sawRetriedReplacement, sawOutdatedCurrent bool
	for _, replica := range replicasResp.GetReplicas() {
		if replica.GetID() == originalReplicaID {
			sawOutdatedCurrent = replica.GetOutdated()
			continue
		}
		if replica.GetLifecycleRetry() != nil &&
			replica.GetDiagnosticCode() == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR {
			sawRetriedReplacement = true
		}
	}
	if !sawOutdatedCurrent {
		t.Fatal("original replica not marked outdated during blocked registry rollout")
	}
	if !sawRetriedReplacement {
		t.Fatal("retrying replacement replica missing registry auth diagnostic")
	}

	eventsResp, err := public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: createResp.GetService().GetID(),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents() error = %v", err)
	}
	var sawRolloutStarted, sawReplacementBlocked, sawServiceDegraded bool
	for _, event := range eventsResp.GetEvents() {
		switch event.GetType() {
		case servicev1.ServiceEventType_SERVICE_EVENT_TYPE_ROLLOUT_STARTED:
			sawRolloutStarted = true
		case servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED:
			if event.GetDiagnosticCode() == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR {
				sawReplacementBlocked = true
			}
		case servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED:
			sawServiceDegraded = true
		}
	}
	if !sawRolloutStarted || sawReplacementBlocked || sawServiceDegraded {
		t.Fatalf("service events do not match active retry semantics: %#v", eventsResp.GetEvents())
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_ROLLOUT_STARTED); got != 1 {
		t.Fatalf("ROLLOUT_STARTED count = %d, want 1", got)
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED); got != 0 {
		t.Fatalf("REPLACEMENT_BLOCKED count = %d, want 0 while retry remains active", got)
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED); got != 0 {
		t.Fatalf("SERVICE_DEGRADED count = %d, want 0 while retry remains active", got)
	}

	app.reconcileV1()

	eventsResp, err = public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: createResp.GetService().GetID(),
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents(after reconcile) error = %v", err)
	}
	if got := countServiceEvents(eventsResp.GetEvents(), servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED); got != 0 {
		t.Fatalf("SERVICE_DEGRADED count after repeated reconcile = %d, want 0 while retry remains active", got)
	}
}
