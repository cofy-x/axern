package app

import (
	"context"
	"testing"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestServiceWatchUsesOneListenerAndStreamsCommittedVersions(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	environment := createDefaultEnvironment(t, app)
	created, err := app.PublicV1Handler().CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: environment.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	serviceID := created.GetService().GetID()
	version := created.GetService().GetVersion()

	watches := make([]servicekernel.WatchStream, 0, 8)
	for range 8 {
		watch, err := app.servicePG.Watch(context.Background(), serviceID, version)
		if err != nil {
			t.Fatalf("Watch() error = %v", err)
		}
		watches = append(watches, watch)
	}
	defer func() {
		for _, watch := range watches {
			watch.Close()
		}
	}()
	if acquired := app.db.Pool().Stat().AcquiredConns(); acquired != 0 {
		t.Fatalf("acquired query-pool connections = %d, want 0 with the dedicated listener", acquired)
	}

	updatedAt := time.Now().UTC()
	if _, err := app.servicePG.UpdateStatus(
		context.Background(),
		serviceID,
		servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
		"watch test",
		updatedAt,
	); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	for index, watch := range watches {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		updated, err := watch.Next(ctx)
		cancel()
		if err != nil {
			t.Fatalf("watch %d Next() error = %v", index, err)
		}
		if updated.GetVersion() <= version {
			t.Fatalf("watch %d version = %d, want > %d", index, updated.GetVersion(), version)
		}
		if updated.GetMessage() != "watch test" {
			t.Fatalf("watch %d message = %q, want watch test", index, updated.GetMessage())
		}
	}
}

func TestServiceWatchReportsPurgeAsNotFound(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	environment := createDefaultEnvironment(t, app)
	created, err := app.PublicV1Handler().CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: environment.GetID(),
		Replicas:      0,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	serviceID := created.GetService().GetID()
	deleted, err := app.PublicV1Handler().DeleteService(context.Background(), &servicev1.DeleteServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	if deleted.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETING {
		t.Fatalf("DeleteService() status = %v, want DELETING", deleted.GetService().GetStatus())
	}
	if err := app.serviceReconciler.ReconcilePending(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("ReconcilePending(delete) error = %v", err)
	}
	deletedResp, err := app.PublicV1Handler().GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after delete reconciliation) error = %v", err)
	}
	if deletedResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		t.Fatalf("reconciled delete status = %v, want DELETED", deletedResp.GetService().GetStatus())
	}
	watch, err := app.servicePG.Watch(context.Background(), serviceID, deletedResp.GetService().GetVersion())
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer watch.Close()
	if _, err := app.AdminV1Handler().PurgeService(context.Background(), &adminv1.PurgeServiceRequest{ServiceID: serviceID, OperatorReason: "test cleanup"}); err != nil {
		t.Fatalf("PurgeService() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := watch.Next(ctx); grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("Next() error = %v, want NotFound", err)
	}
}
