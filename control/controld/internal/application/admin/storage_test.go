package appadmin

import (
	"context"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
)

func TestStorageControlAuditsRetryBeforeCallingStore(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	store := &fakeStorageBindingStore{binding: &adminkernel.StorageBinding{
		BindingID: "bind-1",
		Status:    storagev1.VolumeStatus_VOLUME_STATUS_BOUND,
	}}
	audits := &fakeStorageAuditRecorder{}
	control := NewStorageControl(store, audits, func() time.Time { return now })

	binding, err := control.RetryStorageBinding(context.Background(), adminkernel.RetryStorageBindingRequest{
		BindingID:      " bind-1 ",
		OperatorReason: " node recovered ",
	})
	if err != nil {
		t.Fatalf("RetryStorageBinding() error = %v", err)
	}
	if binding == nil || binding.BindingID != "bind-1" {
		t.Fatalf("binding = %+v", binding)
	}
	if !audits.called || audits.event.Operation != adminkernel.AuditOperationRetryStorageBinding ||
		audits.event.TargetType != adminkernel.AuditTargetStorageBinding ||
		audits.event.TargetID != "bind-1" ||
		audits.event.OperatorReason != "node recovered" ||
		!audits.event.CreatedAt.Equal(now) {
		t.Fatalf("audit event = %+v", audits.event)
	}
	if store.retry.BindingID != "bind-1" || store.retry.OperatorReason != "node recovered" {
		t.Fatalf("retry request = %+v", store.retry)
	}
}

func TestStorageControlRequiresAuditRecorderForRetry(t *testing.T) {
	control := NewStorageControl(&fakeStorageBindingStore{}, nil, nil)
	_, err := control.RetryStorageBinding(context.Background(), adminkernel.RetryStorageBindingRequest{
		BindingID:      "bind-1",
		OperatorReason: "node recovered",
	})
	if err == nil {
		t.Fatal("RetryStorageBinding() unexpectedly succeeded without audit recorder")
	}
}

type fakeStorageBindingStore struct {
	filter  adminkernel.StorageBindingFilter
	retry   adminkernel.RetryStorageBindingRequest
	binding *adminkernel.StorageBinding
}

func (f *fakeStorageBindingStore) ListStorageBindings(context.Context, adminkernel.StorageBindingFilter) ([]adminkernel.StorageBinding, error) {
	return nil, nil
}

func (f *fakeStorageBindingStore) ListStorageReclaims(context.Context, adminkernel.StorageReclaimFilter) ([]adminkernel.StorageReclaim, error) {
	return nil, nil
}

func (f *fakeStorageBindingStore) RetryStorageBinding(_ context.Context, req adminkernel.RetryStorageBindingRequest) (*adminkernel.StorageBinding, error) {
	f.retry = req
	return f.binding, nil
}

type fakeStorageAuditRecorder struct {
	called bool
	event  adminkernel.AuditEvent
}

func (f *fakeStorageAuditRecorder) RecordAdminAuditEvent(_ context.Context, event adminkernel.AuditEvent) error {
	f.called = true
	f.event = event
	return nil
}
