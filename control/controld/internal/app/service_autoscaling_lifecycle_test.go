package app

import (
	"context"
	"testing"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestPostgresServiceClearAutoscalingPolicyReturnsToManualReplicas(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 27, 9, 15, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 5,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{{
				Name:     "business",
				CronUtc:  "* 9-17 * * 1-5",
				Replicas: 3,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if createResp.GetService().GetReplicas() != 1 {
		t.Fatalf("manual replicas = %d, want 1", createResp.GetService().GetReplicas())
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	if admitted.GetAutoscalingStatus().GetCurrentDesiredReplicas() != 3 {
		t.Fatalf("autoscaling desired = %d, want scheduled target 3", admitted.GetAutoscalingStatus().GetCurrentDesiredReplicas())
	}
	if len(admitted.GetAllocationIds()) != 3 {
		t.Fatalf("allocation_ids after scheduled autoscale = %#v, want 3", admitted.GetAllocationIds())
	}

	updateResp, err := public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:  createResp.GetService().GetID(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"autoscaling_policy"}},
	})
	if err != nil {
		t.Fatalf("UpdateService(clear autoscaling_policy) error = %v", err)
	}
	if updateResp.GetService().GetAutoscalingPolicy() != nil {
		t.Fatalf("autoscaling policy after clear = %+v, want nil", updateResp.GetService().GetAutoscalingPolicy())
	}
	if updateResp.GetService().GetAutoscalingStatus() != nil {
		t.Fatalf("autoscaling status after clear = %+v, want nil", updateResp.GetService().GetAutoscalingStatus())
	}
	if len(updateResp.GetService().GetAllocationIds()) != 1 {
		t.Fatalf("allocation_ids after clearing autoscaling = %#v, want manual replica count 1", updateResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.DeleteRequests) != 2 {
		t.Fatalf("delete requests after clearing autoscaling = %d, want 2 scale-down deletes", len(lifecycle.DeleteRequests))
	}
}

func TestPostgresDeletedAutoscaledServiceDoesNotRecreateReplicas(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 27, 9, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 5,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{{
				Name:     "business",
				CronUtc:  "* 9-17 * * 1-5",
				Replicas: 3,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	if len(admitted.GetAllocationIds()) != 3 {
		t.Fatalf("allocation_ids after scheduled autoscale = %#v, want 3", admitted.GetAllocationIds())
	}
	serviceID := createResp.GetService().GetID()

	deleteResp, err := public.DeleteService(context.Background(), &servicev1.DeleteServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	if deleteResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		t.Fatalf("delete status = %v, want DELETED", deleteResp.GetService().GetStatus())
	}
	if len(deleteResp.GetService().GetAllocationIds()) != 0 {
		t.Fatalf("allocation_ids after delete = %#v, want none", deleteResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.DeleteRequests) != 3 {
		t.Fatalf("delete requests after service delete = %d, want 3", len(lifecycle.DeleteRequests))
	}

	now = now.Add(time.Minute)
	if err := app.serviceReconciler.ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	gotResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after deleted autoscale reconcile) error = %v", err)
	}
	if gotResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		t.Fatalf("status after deleted autoscale reconcile = %v, want DELETED", gotResp.GetService().GetStatus())
	}
	if len(gotResp.GetService().GetAllocationIds()) != 0 {
		t.Fatalf("allocation_ids after deleted autoscale reconcile = %#v, want none", gotResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.CreateRequests) != 3 {
		t.Fatalf("create requests after deleted autoscale reconcile = %d, want no new replicas", len(lifecycle.CreateRequests))
	}
}
