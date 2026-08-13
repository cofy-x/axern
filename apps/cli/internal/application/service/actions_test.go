package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	axernsdk "github.com/cofy-x/axern/sdk/go"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc"
)

func TestDeleteReturnsRequestedDeletionWithoutWatching(t *testing.T) {
	client := &fakeServiceClient{deleteResponse: deleteResponse(deletingService(3, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS))}

	result, err := New(client).Delete(context.Background(), DeleteParams{ServiceID: " svc-1 "})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if result.ServiceID != "svc-1" || result.Service.GetVersion() != 3 {
		t.Fatalf("Delete() result = %+v", result)
	}
	if client.deleteRequest.GetServiceID() != "svc-1" {
		t.Fatalf("DeleteService() id = %q, want normalized svc-1", client.deleteRequest.GetServiceID())
	}
}

func TestDeleteCompleteResponseDoesNotStartWatch(t *testing.T) {
	client := &fakeServiceClient{deleteResponse: deleteResponse(deletingService(4, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE))}
	watcher := &fakeServiceWatcher{}

	result, err := NewWithWatcher(client, watcher).Delete(context.Background(), DeleteParams{ServiceID: "svc-1", Wait: true, WaitTimeout: time.Second})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !serviceDeletionComplete(result.Service) || watcher.calls != 0 {
		t.Fatalf("Delete() result = %+v, watch calls = %d", result, watcher.calls)
	}
}

func TestDeleteWaitsForAuthoritativeCompletionSnapshot(t *testing.T) {
	client := &fakeServiceClient{
		deleteResponse: deleteResponse(deletingService(7, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS)),
	}
	watcher := &fakeServiceWatcher{responses: []*servicev1.Service{
		deletingService(9, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES),
		deletingService(12, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE),
	}}

	result, err := NewWithWatcher(client, watcher).Delete(context.Background(), DeleteParams{ServiceID: "svc-1", Wait: true, WaitTimeout: time.Second})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if result.Service.GetVersion() != 12 || !serviceDeletionComplete(result.Service) {
		t.Fatalf("Delete() result = %+v, want completed version 12", result)
	}
	if watcher.calls != 1 || watcher.serviceID != "svc-1" || watcher.afterVersion != 7 {
		t.Fatalf("WatchService() request = (%q, %d), calls = %d", watcher.serviceID, watcher.afterVersion, watcher.calls)
	}
	if !watcher.watch.closed {
		t.Fatal("service deletion watch was not closed")
	}
	if client.replicaCalls != 0 || client.eventCalls != 0 || client.getCalls != 0 {
		t.Fatalf("delete wait performed unrelated reads: get=%d replicas=%d events=%d", client.getCalls, client.replicaCalls, client.eventCalls)
	}
}

func TestDeleteWaitTimeoutReturnsLastSnapshot(t *testing.T) {
	initial := deletingService(5, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS)
	client := &fakeServiceClient{deleteResponse: deleteResponse(initial)}
	watcher := &fakeServiceWatcher{}

	result, err := NewWithWatcher(client, watcher).Delete(context.Background(), DeleteParams{ServiceID: "svc-1", Wait: true, WaitTimeout: time.Nanosecond})
	if err == nil || !strings.Contains(err.Error(), "deletion continues in the background") {
		t.Fatalf("Delete() error = %v, want background deletion timeout", err)
	}
	if result.Service.GetVersion() != initial.GetVersion() {
		t.Fatalf("Delete() last version = %d, want %d", result.Service.GetVersion(), initial.GetVersion())
	}
}

func TestDeleteWaitHonorsParentCancellation(t *testing.T) {
	client := &fakeServiceClient{deleteResponse: deleteResponse(deletingService(5, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS))}
	watcher := &fakeServiceWatcher{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewWithWatcher(client, watcher).Delete(ctx, DeleteParams{ServiceID: "svc-1", Wait: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete() error = %v, want context canceled", err)
	}
}

func TestDeleteWaitReportsParentDeadlineAsBackgroundDeletion(t *testing.T) {
	client := &fakeServiceClient{deleteResponse: deleteResponse(deletingService(5, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS))}
	watcher := &fakeServiceWatcher{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	result, err := NewWithWatcher(client, watcher).Delete(ctx, DeleteParams{ServiceID: "svc-1", Wait: true})
	if err == nil || !strings.Contains(err.Error(), "command deadline exceeded") || !strings.Contains(err.Error(), "deletion continues in the background") {
		t.Fatalf("Delete() error = %v, want command deadline background deletion error", err)
	}
	if result.Service.GetVersion() != 5 {
		t.Fatalf("Delete() last version = %d, want 5", result.Service.GetVersion())
	}
}

func TestDeleteRejectsMalformedAuthoritativeResponse(t *testing.T) {
	tests := map[string]*servicev1.Service{
		"missing service": nil,
		"wrong id":        {ID: "svc-other", Version: 1, Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING, DeletionStatus: &servicev1.ServiceDeletionStatus{Phase: servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS}},
		"missing status":  {ID: "svc-1", Version: 1, Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING},
		"invalid phase":   {ID: "svc-1", Version: 1, Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING, DeletionStatus: &servicev1.ServiceDeletionStatus{}},
	}
	for name, service := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := requestServiceDeletion(context.Background(), fakeServiceActionClient{response: deleteResponse(service)}, "svc-1")
			if err == nil {
				t.Fatal("requestServiceDeletion() error = nil")
			}
		})
	}
}

func TestDeleteValidatesWaitBeforeRequestSideEffects(t *testing.T) {
	tests := map[string]DeleteParams{
		"missing watcher":  {ServiceID: "svc-1", Wait: true},
		"negative timeout": {ServiceID: "svc-1", Wait: true, WaitTimeout: -time.Second},
		"empty service id": {ServiceID: " ", Wait: false},
	}
	for name, params := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeServiceClient{}
			_, err := New(client).Delete(context.Background(), params)
			if err == nil {
				t.Fatal("Delete() error = nil")
			}
			if client.deleteCalls != 0 {
				t.Fatalf("DeleteService() calls = %d, want 0", client.deleteCalls)
			}
		})
	}
}

func TestDeleteRejectsNonMonotonicWatchSnapshot(t *testing.T) {
	client := &fakeServiceClient{
		deleteResponse: deleteResponse(deletingService(7, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS)),
	}
	watcher := &fakeServiceWatcher{responses: []*servicev1.Service{
		deletingService(7, servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES),
	}}

	result, err := NewWithWatcher(client, watcher).Delete(context.Background(), DeleteParams{ServiceID: "svc-1", Wait: true, WaitTimeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "not newer") {
		t.Fatalf("Delete() error = %v, want non-monotonic snapshot rejection", err)
	}
	if result.Service.GetVersion() != 7 {
		t.Fatalf("Delete() last version = %d, want 7", result.Service.GetVersion())
	}
}

type fakeServiceWatcher struct {
	calls        int
	serviceID    string
	afterVersion int64
	responses    []*servicev1.Service
	watch        *fakeServiceWatch
}

func (f *fakeServiceWatcher) WatchService(ctx context.Context, serviceID string, afterVersion int64) (axernsdk.ServiceWatch, error) {
	f.calls++
	f.serviceID = serviceID
	f.afterVersion = afterVersion
	f.watch = &fakeServiceWatch{ctx: ctx, responses: f.responses}
	return f.watch, nil
}

type fakeServiceWatch struct {
	ctx       context.Context
	responses []*servicev1.Service
	next      int
	closed    bool
}

func (f *fakeServiceWatch) Recv() (*servicev1.Service, error) {
	if f.next < len(f.responses) {
		service := f.responses[f.next]
		f.next++
		return service, nil
	}
	<-f.ctx.Done()
	return nil, f.ctx.Err()
}

func (f *fakeServiceWatch) Close() {
	f.closed = true
}

type fakeServiceActionClient struct {
	response *servicev1.DeleteServiceResponse
}

func (f fakeServiceActionClient) DeleteService(context.Context, *servicev1.DeleteServiceRequest, ...grpc.CallOption) (*servicev1.DeleteServiceResponse, error) {
	return f.response, nil
}

func deleteResponse(service *servicev1.Service) *servicev1.DeleteServiceResponse {
	return &servicev1.DeleteServiceResponse{Service: service}
}

func deletingService(version int64, phase servicev1.ServiceDeletionPhase) *servicev1.Service {
	status := servicev1.ServiceStatus_SERVICE_STATUS_DELETING
	if phase == servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE {
		status = servicev1.ServiceStatus_SERVICE_STATUS_DELETED
	}
	return &servicev1.Service{
		ID:      "svc-1",
		Version: version,
		Status:  status,
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase: phase,
		},
	}
}
