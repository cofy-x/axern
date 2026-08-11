package app

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPostgresAcceptsBoundedRetiringMemoryObservationWithoutWorkloadLeaf(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	registerReadyNode(t, app, "node-a", now)
	environment := createDefaultEnvironment(t, app)
	created, err := app.PublicV1Handler().CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: environment.GetID(),
		Replicas:      1,
		Config: &commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{MemoryBytes: 128 << 20},
			Limits:   &commonv1.ResourceQuantity{MemoryBytes: 256 << 20},
		}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, created.GetService().GetID(), now)
	allocationID := admitted.GetAllocationIds()[0]

	observation := &nodev1.AllocationMemoryObservation{
		AllocationID: allocationID, Attempt: 1, Revision: 1, ObservedAt: timestamppb.New(now),
		RequestBytes: 128 << 20, LimitBytes: 256 << 20, CurrentBytes: 32 << 20, PeakBytes: 64 << 20, PeakAvailable: true,
		CgroupIdentity: "boot=test:mount=test:parent=1:leaf=2", Runtime: "runsc",
		ParentControlsVerified: true, LeafControlsVerified: false, PidRolesVerified: false,
		CleanupState: nodev1.AllocationMemoryCleanupState_ALLOCATION_MEMORY_CLEANUP_STATE_RETIRING,
	}
	if _, err := app.NodeV1Handler().BatchReportAllocationMemoryObservations(context.Background(), &nodev1.BatchReportAllocationMemoryObservationsRequest{
		NodeID: "node-a", NodeAuthToken: "test-node-token", Observations: []*nodev1.AllocationMemoryObservation{observation},
	}); err != nil {
		t.Fatalf("BatchReportAllocationMemoryObservations(retiring) error = %v", err)
	}

	var cleanupState string
	var leafVerified bool
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT observation->>'cleanup_state', COALESCE((observation->>'leaf_controls_verified')::BOOLEAN, FALSE)
		FROM allocation_memory_observations WHERE allocation_id = $1
	`, allocationID).Scan(&cleanupState, &leafVerified); err != nil {
		t.Fatalf("load retiring memory observation: %v", err)
	}
	if cleanupState != "ALLOCATION_MEMORY_CLEANUP_STATE_RETIRING" || leafVerified {
		t.Fatalf("stored retiring observation = state:%q leaf_verified:%t", cleanupState, leafVerified)
	}
}
