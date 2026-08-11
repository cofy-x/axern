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

func TestPostgresServiceEnvironmentUpdateRollsReplacement(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 15, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	oldEnv := createImageEnvironment(t, app, "docker.io/library/nginx:1.27")
	newEnv := createImageEnvironment(t, app, "docker.io/library/nginx:1.28")

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: oldEnv.GetID(),
		Replicas:      1,
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/sleep", "300"}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	originalAllocationID := admitted.GetAllocationIds()[0]
	if got := lifecycle.CreateRequests[0].GetConfig().GetEnvironmentID(); got != oldEnv.GetID() {
		t.Fatalf("initial lifecycle environment_id = %q, want %q", got, oldEnv.GetID())
	}
	if got := lifecycle.CreateRequests[0].GetConfig().GetImageDigest(); got != oldEnv.GetResolvedTemplate().GetImageDescriptor().GetDigest() {
		t.Fatalf("initial lifecycle image digest = %q, want %q", got, oldEnv.GetResolvedTemplate().GetImageDescriptor().GetDigest())
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
		t.Fatalf("GetService(before environment update) error = %v", err)
	}

	updateResp, err := public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:       createResp.GetService().GetID(),
		ExpectedVersion: currentResp.GetService().GetVersion(),
		EnvironmentID:   stringptr(newEnv.GetID()),
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"environment_id"}},
	})
	if err != nil {
		t.Fatalf("UpdateService(environment_id) error = %v", err)
	}
	reconcileAllocationLifecycle(t, app, now)
	if updateResp.GetService().GetEnvironmentID() != newEnv.GetID() {
		t.Fatalf("updated environment_id = %q, want %q", updateResp.GetService().GetEnvironmentID(), newEnv.GetID())
	}
	if updateResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("status after environment update = %v, want RECONCILING", updateResp.GetService().GetStatus())
	}
	if len(updateResp.GetService().GetAllocationIds()) != 2 {
		t.Fatalf("allocation_ids after environment update = %#v, want surge to 2", updateResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.CreateRequests) != 2 {
		t.Fatalf("create requests after environment update = %d, want 2", len(lifecycle.CreateRequests))
	}
	if got := lifecycle.CreateRequests[1].GetConfig().GetEnvironmentID(); got != newEnv.GetID() {
		t.Fatalf("replacement lifecycle environment_id = %q, want %q", got, newEnv.GetID())
	}
	if got := lifecycle.CreateRequests[1].GetConfig().GetImageDigest(); got != newEnv.GetResolvedTemplate().GetImageDescriptor().GetDigest() {
		t.Fatalf("replacement lifecycle image digest = %q, want %q", got, newEnv.GetResolvedTemplate().GetImageDescriptor().GetDigest())
	}
	outdatedResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: createResp.GetService().GetID(),
		Filter:    &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_OUTDATED},
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(outdated after environment update) error = %v", err)
	}
	if len(outdatedResp.GetReplicas()) != 1 || outdatedResp.GetReplicas()[0].GetID() != originalAllocationID {
		t.Fatalf("outdated replicas after environment update = %#v, want original allocation %q", outdatedResp.GetReplicas(), originalAllocationID)
	}
	updatedResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: createResp.GetService().GetID(),
		Filter:    &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UPDATED},
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(updated after environment update) error = %v", err)
	}
	if len(updatedResp.GetReplicas()) != 1 {
		t.Fatalf("updated replicas after environment update = %#v, want 1 replacement replica", updatedResp.GetReplicas())
	}

	replacementAllocationID := ""
	for _, allocationID := range updateResp.GetService().GetAllocationIds() {
		if allocationID != originalAllocationID {
			replacementAllocationID = allocationID
			break
		}
	}
	if replacementAllocationID == "" {
		t.Fatal("replacement allocation id not found after environment update")
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
		t.Fatalf("GetService(after environment rollout) error = %v", err)
	}
	if gotResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("status after environment rollout = %v, want READY", gotResp.GetService().GetStatus())
	}
	if gotResp.GetService().GetEnvironmentID() != newEnv.GetID() {
		t.Fatalf("environment_id after environment rollout = %q, want %q", gotResp.GetService().GetEnvironmentID(), newEnv.GetID())
	}
	if len(gotResp.GetService().GetAllocationIds()) != 1 || gotResp.GetService().GetAllocationIds()[0] != replacementAllocationID {
		t.Fatalf("allocation_ids after environment rollout = %#v, want only replacement %q", gotResp.GetService().GetAllocationIds(), replacementAllocationID)
	}
}
