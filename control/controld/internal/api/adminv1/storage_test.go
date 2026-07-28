package adminv1

import (
	"context"
	"errors"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListStorageBindingsMapsFilterAndResponse(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	store := &fakeStorageAdmin{bindings: []adminkernel.StorageBinding{{
		BindingID:    "bind-1",
		ClaimID:      "claim-1",
		Namespace:    "default",
		ClaimName:    "data",
		WorkloadID:   "svc-1",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Status:       storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
		Message:      "publish failed",
		CreatedAt:    now,
		UpdatedAt:    now,
	}}}
	srv := New(Dependencies{Storage: store})

	resp, err := srv.ListStorageBindings(context.Background(), &adminv1.ListStorageBindingsRequest{
		Filter: &adminv1.StorageBindingFilter{
			Statuses:     []storagev1.VolumeStatus{storagev1.VolumeStatus_VOLUME_STATUS_FAILED},
			Namespace:    "default",
			ClaimName:    "data",
			WorkloadID:   "svc-1",
			AllocationID: "alloc-1",
			NodeID:       "node-a",
		},
		Limit: 7,
	})
	if err != nil {
		t.Fatalf("ListStorageBindings() error = %v", err)
	}
	if store.filter.Limit != 7 || store.filter.Namespace != "default" || len(store.filter.Statuses) != 1 {
		t.Fatalf("filter = %+v", store.filter)
	}
	got := resp.GetBindings()[0]
	if got.GetBindingID() != "bind-1" || got.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_FAILED || got.GetUpdatedAt().AsTime() != now {
		t.Fatalf("binding = %+v", got)
	}
}

func TestRetryStorageBindingMapsRequest(t *testing.T) {
	store := &fakeStorageAdmin{binding: &adminkernel.StorageBinding{
		BindingID: "bind-1",
		Status:    storagev1.VolumeStatus_VOLUME_STATUS_BOUND,
	}}
	srv := New(Dependencies{Storage: store})

	resp, err := srv.RetryStorageBinding(context.Background(), &adminv1.RetryStorageBindingRequest{
		BindingID:      "bind-1",
		OperatorReason: "node recovered",
	})
	if err != nil {
		t.Fatalf("RetryStorageBinding() error = %v", err)
	}
	if store.retry.BindingID != "bind-1" || store.retry.OperatorReason != "node recovered" {
		t.Fatalf("retry request = %+v", store.retry)
	}
	if resp.GetBinding().GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_BOUND {
		t.Fatalf("binding status = %s", resp.GetBinding().GetStatus())
	}
}

func TestStorageAdminUnavailable(t *testing.T) {
	srv := New(Dependencies{})
	_, err := srv.ListStorageBindings(context.Background(), &adminv1.ListStorageBindingsRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("ListStorageBindings() code = %s, want unavailable", status.Code(err))
	}
	_, err = srv.RetryStorageBinding(context.Background(), &adminv1.RetryStorageBindingRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("RetryStorageBinding() code = %s, want unavailable", status.Code(err))
	}
}

type fakeStorageAdmin struct {
	filter   adminkernel.StorageBindingFilter
	retry    adminkernel.RetryStorageBindingRequest
	bindings []adminkernel.StorageBinding
	binding  *adminkernel.StorageBinding
	err      error
}

func (f *fakeStorageAdmin) ListStorageBindings(_ context.Context, filter adminkernel.StorageBindingFilter) ([]adminkernel.StorageBinding, error) {
	f.filter = filter
	if f.err != nil {
		return nil, f.err
	}
	return f.bindings, nil
}

func (f *fakeStorageAdmin) ListStorageReclaims(context.Context, adminkernel.StorageReclaimFilter) ([]adminkernel.StorageReclaim, error) {
	return nil, nil
}

func (f *fakeStorageAdmin) RetryStorageBinding(_ context.Context, req adminkernel.RetryStorageBindingRequest) (*adminkernel.StorageBinding, error) {
	f.retry = req
	if f.err != nil {
		return nil, f.err
	}
	if f.binding == nil {
		return nil, errors.New("missing binding")
	}
	return f.binding, nil
}
