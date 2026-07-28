package app

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestPostgresServiceReadinessProbeSemantics(t *testing.T) {
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
		ReadinessProbe: &servicev1.ServiceProbe{
			Action: &servicev1.ServiceProbe_Http{
				Http: &servicev1.HttpProbe{
					Port: 8080,
					Path: "/readyz",
				},
			},
			Period:           durationpb.New(5 * time.Second),
			Timeout:          durationpb.New(2 * time.Second),
			SuccessThreshold: 1,
			FailureThreshold: 1,
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	serviceID := createResp.GetService().GetID()
	admitted := reconcileCreatedService(t, app, serviceID, now)
	replicaID := admitted.GetAllocationIds()[0]
	if createResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("create status = %v, want RECONCILING", createResp.GetService().GetStatus())
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID:     replicaID,
			Attempt:          1,
			Status:           commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:            false,
			ReadinessMessage: "waiting for readiness probe",
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(RUNNING ready=false) error = %v", err)
	}

	serviceResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after ready=false) error = %v", err)
	}
	if serviceResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("status after ready=false = %v, want RECONCILING", serviceResp.GetService().GetStatus())
	}
	if serviceResp.GetService().GetReadyReplicas() != 0 || serviceResp.GetService().GetUnhealthyReplicas() != 0 {
		t.Fatalf("counters after ready=false = ready:%d unhealthy:%d, want 0/0", serviceResp.GetService().GetReadyReplicas(), serviceResp.GetService().GetUnhealthyReplicas())
	}
	if serviceResp.GetService().GetMessage() != "waiting for readiness probe" {
		t.Fatalf("message after ready=false = %q, want readiness message", serviceResp.GetService().GetMessage())
	}

	replicaResp, err := public.GetServiceReplica(context.Background(), &servicev1.GetServiceReplicaRequest{
		ServiceID: serviceID,
		ReplicaID: replicaID,
	})
	if err != nil {
		t.Fatalf("GetServiceReplica(after ready=false) error = %v", err)
	}
	if replicaResp.GetReplica().GetReady() {
		t.Fatal("replica unexpectedly marked ready")
	}
	if replicaResp.GetReplica().GetReadinessMessage() != "waiting for readiness probe" {
		t.Fatalf("replica readiness message = %q, want propagated readiness message", replicaResp.GetReplica().GetReadinessMessage())
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: replicaID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(RUNNING ready=true) error = %v", err)
	}

	serviceResp, err = public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after ready=true) error = %v", err)
	}
	if serviceResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("status after ready=true = %v, want READY", serviceResp.GetService().GetStatus())
	}
	if serviceResp.GetService().GetReadyReplicas() != 1 {
		t.Fatalf("ready_replicas after ready=true = %d, want 1", serviceResp.GetService().GetReadyReplicas())
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID:     replicaID,
			Attempt:          1,
			Status:           commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:            false,
			ReadinessMessage: "dependency warming",
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(RUNNING ready flips false) error = %v", err)
	}

	serviceResp, err = public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after ready flips false) error = %v", err)
	}
	if serviceResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("status after ready flips false = %v, want RECONCILING", serviceResp.GetService().GetStatus())
	}
	if serviceResp.GetService().GetUnhealthyReplicas() != 0 {
		t.Fatalf("unhealthy_replicas after ready flips false = %d, want 0", serviceResp.GetService().GetUnhealthyReplicas())
	}

	eventsResp, err := public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: serviceID,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents() error = %v", err)
	}
	for _, event := range eventsResp.GetEvents() {
		if event.GetType() == servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED {
			t.Fatalf("unexpected degraded event for readiness-only transition: %#v", event)
		}
	}
}

func TestPostgresServiceLivenessFailureTriggersReplacement(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 17, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
		ReadinessProbe: &servicev1.ServiceProbe{
			Action: &servicev1.ServiceProbe_Http{Http: &servicev1.HttpProbe{Port: 8080, Path: "/readyz"}},
		},
		LivenessProbe: &servicev1.ServiceProbe{
			Action: &servicev1.ServiceProbe_Http{Http: &servicev1.HttpProbe{Port: 8080, Path: "/livez"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	serviceID := createResp.GetService().GetID()
	admitted := reconcileCreatedService(t, app, serviceID, now)
	replicaID := admitted.GetAllocationIds()[0]

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: replicaID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(RUNNING ready=true) error = %v", err)
	}

	serviceResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(ready) error = %v", err)
	}
	if serviceResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("status before liveness failure = %v, want READY", serviceResp.GetService().GetStatus())
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: replicaID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			Message:      "liveness probe failed: http probe returned 500",
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(FAILED liveness) error = %v", err)
	}

	serviceResp, err = public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after liveness failure) error = %v", err)
	}
	if serviceResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("status after liveness failure = %v, want DEGRADED", serviceResp.GetService().GetStatus())
	}
	if serviceResp.GetService().GetUnhealthyReplicas() != 1 {
		t.Fatalf("unhealthy_replicas after liveness failure = %d, want 1", serviceResp.GetService().GetUnhealthyReplicas())
	}

	eventsResp, err := public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: serviceID,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents(after liveness failure) error = %v", err)
	}
	var sawLivenessFailed bool
	for _, event := range eventsResp.GetEvents() {
		if event.GetType() == servicev1.ServiceEventType_SERVICE_EVENT_TYPE_LIVENESS_FAILED {
			sawLivenessFailed = true
			break
		}
	}
	if !sawLivenessFailed {
		t.Fatalf("service events missing liveness failure marker: %#v", eventsResp.GetEvents())
	}

	app.reconcileV1()

	replicasResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("ListServiceReplicas(after reconcile) error = %v", err)
	}
	var replacementID string
	for _, replica := range replicasResp.GetReplicas() {
		if replica.GetID() != replicaID && !replica.GetEnded() {
			replacementID = replica.GetID()
		}
	}
	if replacementID == "" {
		t.Fatalf("replacement replica not found after reconcile: %#v", replicasResp.GetReplicas())
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID:     replacementID,
			Attempt:          1,
			Status:           commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:            false,
			ReadinessMessage: "warming up replacement",
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(replacement ready=false) error = %v", err)
	}

	serviceResp, err = public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(replacement not ready) error = %v", err)
	}
	if serviceResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("status with replacement not ready = %v, want DEGRADED while unhealthy history remains", serviceResp.GetService().GetStatus())
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: replacementID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(replacement ready=true) error = %v", err)
	}

	serviceResp, err = public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(recovered) error = %v", err)
	}
	if serviceResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("status after replacement ready = %v, want READY", serviceResp.GetService().GetStatus())
	}
}
