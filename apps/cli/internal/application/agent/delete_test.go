package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDeleteWorkspaceRequiresSuspendedServiceAndDeletesVolumes(t *testing.T) {
	service := &servicev1.Service{
		ID: "svc-1", Version: 7, Replicas: 0,
		Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Labels: workspaceLabels("project-a", "codex", "codex"),
	}
	complete := &servicev1.Service{
		ID: "svc-1", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETED,
		Labels: service.GetLabels(), DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:    servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE,
			ClaimIds: []string{"claim-1"}, Message: "workspace data deleted",
			CompletedAt: timestamppb.New(time.Unix(10, 0)),
		},
	}
	client := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{service}},
		deleteResp:       &servicev1.DeleteServiceResponse{Service: complete},
	}
	result, err := (Control{}).DeleteWorkspace(context.Background(), client, "project-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if client.deleteReq.GetExpectedVersion() != 7 || !client.deleteReq.GetRequireSuspended() || client.deleteReq.GetVolumeDisposition() != servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE {
		t.Fatalf("delete request = %#v", client.deleteReq)
	}
	if result.State != LifecycleDeleted || result.ServiceID != "svc-1" || len(result.ClaimIDs) != 1 || result.CompletedAt.IsZero() {
		t.Fatalf("delete result = %#v", result)
	}
}

func TestDeleteWorkspaceRejectsRunningService(t *testing.T) {
	client := &fakeServiceClient{listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{{
		ID: "svc-1", Replicas: 1, Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Labels: workspaceLabels("project-a", "codex", "codex"),
	}}}}
	_, err := (Control{}).DeleteWorkspace(context.Background(), client, "project-a", time.Second)
	if err == nil || !strings.Contains(err.Error(), "stop it before deletion") {
		t.Fatalf("error = %v", err)
	}
	if client.deleteReq != nil {
		t.Fatal("running workspace reached DeleteService")
	}
}

func TestDeleteWorkspaceRetriesOneVersionConflict(t *testing.T) {
	serviceV17 := &servicev1.Service{
		ID: "svc-1", Version: 17, Replicas: 0,
		Labels: workspaceLabels("project-a", "codex", "codex"),
	}
	serviceV18 := &servicev1.Service{
		ID: "svc-1", Version: 18, Replicas: 0,
		Labels: serviceV17.GetLabels(),
	}
	complete := &servicev1.Service{
		ID: "svc-1", Version: 19, Labels: serviceV17.GetLabels(),
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase: servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE,
		},
	}
	client := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{serviceV17}},
		getResp:          &servicev1.GetServiceResponse{Service: serviceV18},
		deleteErrs:       []error{status.Error(codes.Aborted, "version mismatch"), nil},
		deleteResponses:  []*servicev1.DeleteServiceResponse{nil, {Service: complete}},
	}
	result, err := (Control{}).DeleteWorkspace(context.Background(), client, "project-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != LifecycleDeleted || len(client.deleteReqs) != 2 || client.deleteReqs[0].GetExpectedVersion() != 17 || client.deleteReqs[1].GetExpectedVersion() != 18 {
		t.Fatalf("result=%#v delete requests=%#v", result, client.deleteReqs)
	}
}

func TestDeleteWorkspaceVersionConflictRejectsResumedService(t *testing.T) {
	serviceV17 := &servicev1.Service{ID: "svc-1", Version: 17, Replicas: 0, Labels: workspaceLabels("project-a", "codex", "codex")}
	serviceV18 := &servicev1.Service{ID: "svc-1", Version: 18, Replicas: 1, Labels: serviceV17.GetLabels()}
	client := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{serviceV17}},
		getResp:          &servicev1.GetServiceResponse{Service: serviceV18},
		deleteErrs:       []error{status.Error(codes.Aborted, "version mismatch")},
	}
	_, err := (Control{}).DeleteWorkspace(context.Background(), client, "project-a", time.Second)
	if err == nil || !strings.Contains(err.Error(), "is running") || len(client.deleteReqs) != 1 {
		t.Fatalf("error=%v delete requests=%#v", err, client.deleteReqs)
	}
}

func TestDeleteWorkspaceContinuesExistingDeletionAndTimesOutLocally(t *testing.T) {
	deleting := &servicev1.Service{
		ID: "svc-1", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
		Labels: workspaceLabels("project-a", "codex", "codex"),
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:    servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES,
			ClaimIds: []string{"claim-1"},
		},
	}
	client := &fakeServiceClient{listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{deleting}}}
	result, err := (Control{}).DeleteWorkspace(context.Background(), client, "project-a", time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "deletion continues in the background") {
		t.Fatalf("error = %v", err)
	}
	if result.State != LifecycleDeleting || result.ServiceID != "svc-1" || client.deleteReq != nil {
		t.Fatalf("result = %#v delete=%#v", result, client.deleteReq)
	}
}

func TestDeleteWorkspaceCompletedTombstoneIsIdempotent(t *testing.T) {
	completed := &servicev1.Service{
		ID: "svc-old", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETED,
		Labels: workspaceLabels("project-a", "codex", "codex"),
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:       servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE,
			CompletedAt: timestamppb.Now(),
		},
	}
	client := &fakeServiceClient{listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{completed}}}
	result, err := (Control{}).DeleteWorkspace(context.Background(), client, "project-a", time.Second)
	if err != nil || result.State != LifecycleDeleted || client.deleteReq != nil {
		t.Fatalf("result=%#v err=%v delete=%#v", result, err, client.deleteReq)
	}
}

func TestDeleteWorkspaceRejectsDeletingAndActiveDuplicate(t *testing.T) {
	deleting := &servicev1.Service{
		ID: "svc-old", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
		Labels:         workspaceLabels("project-a", "codex", "codex"),
		DeletionStatus: &servicev1.ServiceDeletionStatus{Phase: servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES},
	}
	active := &servicev1.Service{
		ID: "svc-new", Status: servicev1.ServiceStatus_SERVICE_STATUS_READY, Replicas: 0,
		Labels: workspaceLabels("project-a", "codex", "codex"),
	}
	client := &fakeServiceClient{listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{deleting, active}}}
	_, err := (Control{}).DeleteWorkspace(context.Background(), client, "project-a", time.Second)
	if err == nil || !strings.Contains(err.Error(), "multiple active services") {
		t.Fatalf("error = %v", err)
	}
}
