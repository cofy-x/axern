package controldtest

import (
	"context"
	"sync"

	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type FakeNodeLifecycleClient struct {
	mu                 sync.Mutex
	CreateRequests     []*privatenodev1.CreateAllocationRequest
	DeleteRequests     []*privatenodev1.DeleteAllocationRequest
	CreateErr          error
	DeleteErr          error
	StatusErr          error
	StatusByID         map[string]*privatenodev1.GetAllocationStatusResponse
	KeepDeletedVisible bool
	deleted            map[string]bool
}

func (f *FakeNodeLifecycleClient) CreateAllocation(ctx context.Context, target string, req *privatenodev1.CreateAllocationRequest) (*privatenodev1.CreateAllocationResponse, error) {
	_ = ctx
	_ = target
	f.mu.Lock()
	f.CreateRequests = append(f.CreateRequests, proto.Clone(req).(*privatenodev1.CreateAllocationRequest))
	err := f.CreateErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &privatenodev1.CreateAllocationResponse{
		AllocationID: req.GetAllocationID(),
		Attempt:      req.GetAttempt(),
	}, nil
}

func (f *FakeNodeLifecycleClient) DeleteAllocation(ctx context.Context, target string, req *privatenodev1.DeleteAllocationRequest) (*privatenodev1.DeleteAllocationResponse, error) {
	_ = ctx
	_ = target
	f.mu.Lock()
	f.DeleteRequests = append(f.DeleteRequests, proto.Clone(req).(*privatenodev1.DeleteAllocationRequest))
	if f.DeleteErr == nil && !f.KeepDeletedVisible {
		if f.deleted == nil {
			f.deleted = make(map[string]bool)
		}
		f.deleted[req.GetAllocationID()] = true
	}
	err := f.DeleteErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &privatenodev1.DeleteAllocationResponse{}, nil
}

func (f *FakeNodeLifecycleClient) DeleteVolume(context.Context, string, *privatenodev1.DeleteVolumeRequest) (*privatenodev1.DeleteVolumeResponse, error) {
	return &privatenodev1.DeleteVolumeResponse{}, nil
}

func (f *FakeNodeLifecycleClient) GetAllocationStatus(ctx context.Context, target string, req *privatenodev1.GetAllocationStatusRequest) (*privatenodev1.GetAllocationStatusResponse, error) {
	_ = ctx
	_ = target
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StatusErr != nil {
		return nil, f.StatusErr
	}
	if f.deleted[req.GetAllocationID()] {
		return nil, grpcstatus.Error(codes.NotFound, "allocation not found")
	}
	if resp, ok := f.StatusByID[req.GetAllocationID()]; ok && resp != nil {
		return proto.Clone(resp).(*privatenodev1.GetAllocationStatusResponse), nil
	}
	return &privatenodev1.GetAllocationStatusResponse{}, nil
}

func (f *FakeNodeLifecycleClient) Close() error {
	return nil
}
