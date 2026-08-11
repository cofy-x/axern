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

func TestPostgresServiceConfigUpdateMultiReplicaKeepsAvailability(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 13, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      2,
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	oldAllocations := append([]string(nil), admitted.GetAllocationIds()...)
	for _, allocationID := range oldAllocations {
		if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
			NodeID:        "node-a",
			NodeAuthToken: "test-node-token",
			Observations: []*nodev1.AllocationStatusObservation{{
				AllocationID: allocationID,
				Attempt:      1,
				Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
				Ready:        true,
			}},
		}); err != nil {
			t.Fatalf("BatchReportAllocationStatus(old RUNNING) error = %v", err)
		}
	}
	currentResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(before rollout) error = %v", err)
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
	if len(updateResp.GetService().GetAllocationIds()) != 3 {
		t.Fatalf("allocation_ids after first surge = %#v, want 3", updateResp.GetService().GetAllocationIds())
	}
	firstReplacement := ""
	for _, allocationID := range updateResp.GetService().GetAllocationIds() {
		if allocationID != oldAllocations[0] && allocationID != oldAllocations[1] {
			firstReplacement = allocationID
			break
		}
	}
	if firstReplacement == "" {
		t.Fatal("first replacement allocation id not found")
	}
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: firstReplacement,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(first replacement RUNNING) error = %v", err)
	}

	app.reconcileV1()

	afterFirstDelete, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after first delete) error = %v", err)
	}
	if len(afterFirstDelete.GetService().GetAllocationIds()) != 2 {
		t.Fatalf("allocation_ids after first delete = %#v, want 2", afterFirstDelete.GetService().GetAllocationIds())
	}
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests after first delete = %d, want 1", len(lifecycle.DeleteRequests))
	}
	deletedOld := lifecycle.DeleteRequests[0].GetAllocationID()
	survivingOld := oldAllocations[0]
	if deletedOld == survivingOld {
		survivingOld = oldAllocations[1]
	}

	app.reconcileV1()

	afterSecondSurge, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after second surge) error = %v", err)
	}
	if len(afterSecondSurge.GetService().GetAllocationIds()) != 3 {
		t.Fatalf("allocation_ids after second surge = %#v, want 3", afterSecondSurge.GetService().GetAllocationIds())
	}
	if afterSecondSurge.GetService().GetMessage() == "" {
		t.Fatal("message after second surge = empty, want rollout progress message")
	}
	if afterSecondSurge.GetService().GetRolloutStatus() == nil || afterSecondSurge.GetService().GetRolloutStatus().GetOutdatedReplicas() != 1 {
		t.Fatalf("rollout status after second surge = %+v, want outdated=1", afterSecondSurge.GetService().GetRolloutStatus())
	}
	secondReplacement := ""
	for _, allocationID := range afterSecondSurge.GetService().GetAllocationIds() {
		if allocationID != firstReplacement && allocationID != survivingOld {
			secondReplacement = allocationID
			break
		}
	}
	if secondReplacement == "" {
		t.Fatal("second replacement allocation id not found")
	}

	app.reconcileV1()

	stillRolling, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(while second replacement pending) error = %v", err)
	}
	if len(stillRolling.GetService().GetAllocationIds()) != 3 {
		t.Fatalf("allocation_ids while second replacement pending = %#v, want 3", stillRolling.GetService().GetAllocationIds())
	}
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests while second replacement pending = %d, want still 1", len(lifecycle.DeleteRequests))
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: secondReplacement,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(second replacement RUNNING) error = %v", err)
	}

	app.reconcileV1()

	finalResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after rollout complete) error = %v", err)
	}
	if finalResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("final status = %v, want READY", finalResp.GetService().GetStatus())
	}
	if finalResp.GetService().GetMessage() != "" {
		t.Fatalf("final message = %q, want empty", finalResp.GetService().GetMessage())
	}
	if finalResp.GetService().GetRolloutStatus() != nil {
		t.Fatalf("final rollout status = %+v, want nil", finalResp.GetService().GetRolloutStatus())
	}
	if len(finalResp.GetService().GetAllocationIds()) != 2 {
		t.Fatalf("final allocation_ids = %#v, want 2 new replicas", finalResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.DeleteRequests) != 2 {
		t.Fatalf("final delete requests = %d, want 2", len(lifecycle.DeleteRequests))
	}
}

func TestPostgresServiceConfigUpdateHonorsCustomRolloutPolicy(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 14, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      2,
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}},
		RolloutPolicy: &servicev1.ServiceRolloutPolicy{MaxSurge: 0, MaxUnavailable: 1},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if createResp.GetService().GetRolloutPolicy().GetMaxSurge() != 0 || createResp.GetService().GetRolloutPolicy().GetMaxUnavailable() != 1 {
		t.Fatalf("create rollout policy = %+v, want max_surge=0 max_unavailable=1", createResp.GetService().GetRolloutPolicy())
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	oldAllocations := append([]string(nil), admitted.GetAllocationIds()...)
	for _, allocationID := range oldAllocations {
		if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
			NodeID:        "node-a",
			NodeAuthToken: "test-node-token",
			Observations: []*nodev1.AllocationStatusObservation{{
				AllocationID: allocationID,
				Attempt:      1,
				Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
				Ready:        true,
			}},
		}); err != nil {
			t.Fatalf("BatchReportAllocationStatus(old RUNNING) error = %v", err)
		}
	}
	currentResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(before rollout) error = %v", err)
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
	if len(updateResp.GetService().GetAllocationIds()) != 1 {
		t.Fatalf("allocation_ids after rollout start = %#v, want 1 after no-surge deletion", updateResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests after rollout start = %d, want 1", len(lifecycle.DeleteRequests))
	}
	if len(lifecycle.CreateRequests) != 2 {
		t.Fatalf("create requests after rollout start = %d, want still 2", len(lifecycle.CreateRequests))
	}

	app.reconcileV1()

	afterReplacementAdmit, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after replacement admit) error = %v", err)
	}
	if len(afterReplacementAdmit.GetService().GetAllocationIds()) != 2 {
		t.Fatalf("allocation_ids after replacement admit = %#v, want 2", afterReplacementAdmit.GetService().GetAllocationIds())
	}
	if afterReplacementAdmit.GetService().GetRolloutStatus() == nil || !afterReplacementAdmit.GetService().GetRolloutStatus().GetInProgress() {
		t.Fatalf("rollout status after replacement admit = %+v, want in_progress", afterReplacementAdmit.GetService().GetRolloutStatus())
	}
	if len(lifecycle.CreateRequests) != 3 {
		t.Fatalf("create requests after replacement admit = %d, want 3", len(lifecycle.CreateRequests))
	}
}

func TestPostgresServiceScaleDownDuringRolloutDrainsBeforeAdmittingReplacement(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
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
		Replicas:      2,
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	oldAllocations := append([]string(nil), admitted.GetAllocationIds()...)
	for _, allocationID := range oldAllocations {
		if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
			NodeID:        "node-a",
			NodeAuthToken: "test-node-token",
			Observations: []*nodev1.AllocationStatusObservation{{
				AllocationID: allocationID,
				Attempt:      1,
				Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
				Ready:        true,
			}},
		}); err != nil {
			t.Fatalf("BatchReportAllocationStatus(old RUNNING) error = %v", err)
		}
	}
	currentResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(before rollout) error = %v", err)
	}

	updateResp, err := public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:       createResp.GetService().GetID(),
		ExpectedVersion: currentResp.GetService().GetVersion(),
		Replicas:        int32ptr(1),
		Config:          &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"replicas", "config"}},
	})
	if err != nil {
		t.Fatalf("UpdateService(replicas+config) error = %v", err)
	}
	reconcileAllocationLifecycle(t, app, now)
	if updateResp.GetService().GetReplicas() != 1 {
		t.Fatalf("replicas after update = %d, want 1", updateResp.GetService().GetReplicas())
	}
	if len(updateResp.GetService().GetAllocationIds()) != 1 {
		t.Fatalf("allocation_ids after scale-down rollout step = %#v, want one surviving old replica", updateResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.CreateRequests) != 2 {
		t.Fatalf("create requests after scale-down rollout step = %d, want no replacement admitted yet", len(lifecycle.CreateRequests))
	}
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests after scale-down rollout step = %d, want one old replica drained", len(lifecycle.DeleteRequests))
	}
	if updateResp.GetService().GetRolloutStatus() == nil || updateResp.GetService().GetRolloutStatus().GetOutdatedReplicas() != 1 {
		t.Fatalf("rollout status after scale-down rollout step = %+v, want one outdated survivor", updateResp.GetService().GetRolloutStatus())
	}

	app.reconcileV1()

	afterAdmitResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after replacement admit) error = %v", err)
	}
	if len(afterAdmitResp.GetService().GetAllocationIds()) != 2 {
		t.Fatalf("allocation_ids after replacement admit = %#v, want old survivor plus replacement", afterAdmitResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.CreateRequests) != 3 {
		t.Fatalf("create requests after replacement admit = %d, want one replacement", len(lifecycle.CreateRequests))
	}
}
