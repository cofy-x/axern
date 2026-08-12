package app

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestPostgresServiceReadFailsWhenRolloutEnrichmentFails(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 11, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)

	if _, err := app.db.Pool().Exec(context.Background(), `
		UPDATE allocations
		SET config = '{"argv": 42}'::jsonb
		WHERE allocation_id = $1
	`, admitted.GetAllocationIds()[0]); err != nil {
		t.Fatalf("corrupt allocation config: %v", err)
	}

	if _, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{
		ServiceID: createResp.GetService().GetID(),
	}); err == nil {
		t.Fatal("GetService() unexpectedly succeeded with unreadable rollout enrichment state")
	}

	if _, err := public.ListServices(context.Background(), &servicev1.ListServicesRequest{
		Filter: &servicev1.ServiceListFilter{Namespace: "default"},
	}); err == nil {
		t.Fatal("ListServices() unexpectedly succeeded with unreadable rollout enrichment state")
	}
}

func TestPostgresServiceConfigUpdateRollsReplacement(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
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
	originalAllocationID := admitted.GetAllocationIds()[0]
	if got := lifecycle.CreateRequests[0].GetConfig().GetArgv(); len(got) != 1 || got[0] != "/bin/old" {
		t.Fatalf("initial lifecycle argv = %#v, want [/bin/old]", got)
	}
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: originalAllocationID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(original RUNNING) error = %v", err)
	}
	currentResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(before config update) error = %v", err)
	}

	updateResp, err := public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:       createResp.GetService().GetID(),
		ExpectedVersion: currentResp.GetService().GetVersion(),
		Config:          &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"config"}},
	})
	if err != nil {
		t.Fatalf("UpdateService(config) error = %v", err)
	}
	reconcileAllocationLifecycle(t, app, now)
	if updateResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("status after config update = %v, want RECONCILING", updateResp.GetService().GetStatus())
	}
	if updateResp.GetService().GetMessage() == "" {
		t.Fatal("status message after config update = empty, want rollout progress message")
	}
	if updateResp.GetService().GetRolloutStatus() == nil || !updateResp.GetService().GetRolloutStatus().GetInProgress() {
		t.Fatalf("rollout status after config update = %+v, want in_progress", updateResp.GetService().GetRolloutStatus())
	}
	if updateResp.GetService().GetRolloutStatus().GetOutdatedReplicas() != 1 || updateResp.GetService().GetRolloutStatus().GetUpdatedReadyReplicas() != 0 {
		t.Fatalf("rollout status after config update = %+v, want outdated=1 updated_ready=0", updateResp.GetService().GetRolloutStatus())
	}
	if len(updateResp.GetService().GetAllocationIds()) != 2 {
		t.Fatalf("allocation_ids after config update = %#v, want surge to 2", updateResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.CreateRequests) != 2 {
		t.Fatalf("create requests after config update = %d, want 2", len(lifecycle.CreateRequests))
	}
	if got := lifecycle.CreateRequests[1].GetConfig().GetArgv(); len(got) != 1 || got[0] != "/bin/new" {
		t.Fatalf("replacement lifecycle argv = %#v, want [/bin/new]", got)
	}

	replacementAllocationID := ""
	for _, allocationID := range updateResp.GetService().GetAllocationIds() {
		if allocationID != originalAllocationID {
			replacementAllocationID = allocationID
			break
		}
	}
	if replacementAllocationID == "" {
		t.Fatal("replacement allocation id not found after config update")
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: replacementAllocationID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(replacement RUNNING) error = %v", err)
	}

	app.reconcileV1()

	gotResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after rollout) error = %v", err)
	}
	if gotResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("status after rollout = %v, want READY", gotResp.GetService().GetStatus())
	}
	if gotResp.GetService().GetMessage() != "" {
		t.Fatalf("message after rollout = %q, want empty", gotResp.GetService().GetMessage())
	}
	if gotResp.GetService().GetRolloutStatus() != nil {
		t.Fatalf("rollout status after rollout = %+v, want nil", gotResp.GetService().GetRolloutStatus())
	}
	if len(gotResp.GetService().GetAllocationIds()) != 1 || gotResp.GetService().GetAllocationIds()[0] != replacementAllocationID {
		t.Fatalf("allocation_ids after rollout = %#v, want only replacement %q", gotResp.GetService().GetAllocationIds(), replacementAllocationID)
	}
	if len(lifecycle.DeleteRequests) != 1 || lifecycle.DeleteRequests[0].GetAllocationID() != originalAllocationID {
		t.Fatalf("delete requests after rollout = %#v, want original allocation %q deleted", lifecycle.DeleteRequests, originalAllocationID)
	}
}
