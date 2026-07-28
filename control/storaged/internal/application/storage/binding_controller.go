package storage

import (
	"context"
	"fmt"
	"strings"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

func (c *Controller) ResolveVolumeRequirements(ctx context.Context, req *privatestoragev1.ResolveVolumeRequirementsRequest) (*privatestoragev1.ResolveVolumeRequirementsResponse, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	namespace := kernel.NormalizeNamespace(req.GetNamespace())
	out := &privatestoragev1.ResolveVolumeRequirementsResponse{}
	for _, mount := range req.GetMounts() {
		if mount == nil {
			continue
		}
		claim, class, err := c.ensureClaimAndClass(ctx, namespace, req.GetWorkloadID(), req.GetWorkloadType(), mount)
		if err != nil {
			return nil, err
		}
		topology := cloneTopology(claim.GetTopology())
		if binding, ok, err := c.store.GetVolumeBindingForClaim(ctx, claim.GetID()); err != nil {
			return nil, err
		} else if ok {
			topology = cloneTopology(binding.GetResolvedVolume().GetTopology())
		}
		out.Requirements = append(out.Requirements, &privatestoragev1.VolumeRequirement{
			ClaimID:              claim.GetID(),
			ClaimName:            claim.GetName(),
			AccessMode:           claim.GetAccessMode(),
			BindingScope:         claim.GetBindingScope(),
			ConsistencyProfile:   class.GetConsistencyProfile(),
			Topology:             topology,
			RuntimeCompatibility: cloneRuntimeCompatibility(class.GetRuntimeCompatibility()),
			ReclaimPolicy:        claim.GetReclaimPolicy(),
		})
	}
	return out, nil
}

func (c *Controller) ReserveVolumeBinding(ctx context.Context, req *privatestoragev1.ReserveVolumeBindingRequest) (*privatestoragev1.ReserveVolumeBindingResponse, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	namespace := kernel.NormalizeNamespace(req.GetNamespace())
	workloadID := strings.TrimSpace(req.GetWorkloadID())
	workloadType := strings.TrimSpace(req.GetWorkloadType())
	allocationID := strings.TrimSpace(req.GetAllocationID())
	nodeID := strings.TrimSpace(req.GetNodeID())
	if workloadID == "" {
		return nil, fmt.Errorf("storage reserve workload id is required")
	}
	if workloadType == "" {
		return nil, fmt.Errorf("storage reserve workload type is required")
	}
	if allocationID == "" {
		return nil, fmt.Errorf("storage reserve allocation id is required")
	}
	if nodeID == "" {
		return nil, fmt.Errorf("storage reserve node id is required")
	}
	out := &privatestoragev1.ReserveVolumeBindingResponse{}
	for _, mount := range req.GetMounts() {
		if mount == nil {
			continue
		}
		if strings.TrimSpace(mount.GetTarget()) == "" {
			return nil, fmt.Errorf("storage reserve mount target is required")
		}
		claim, class, err := c.ensureClaimAndClass(ctx, namespace, workloadID, workloadType, mount)
		if err != nil {
			return nil, err
		}
		volume, err := c.store.ReserveVolumeBinding(ctx, kernel.VolumeBindingReserve{
			Namespace:    namespace,
			WorkloadID:   workloadID,
			WorkloadType: workloadType,
			AllocationID: allocationID,
			NodeID:       nodeID,
			Mount:        cloneWorkloadVolumeMount(mount),
			Claim:        claim,
			Class:        class,
		})
		if err != nil {
			return nil, err
		}
		out.Volumes = append(out.Volumes, volume)
	}
	return out, nil
}

func (c *Controller) ReleaseVolumeBinding(ctx context.Context, req *privatestoragev1.ReleaseVolumeBindingRequest) (*privatestoragev1.ReleaseVolumeBindingResponse, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	allocationID := strings.TrimSpace(req.GetAllocationID())
	nodeID := strings.TrimSpace(req.GetNodeID())
	if allocationID == "" {
		return nil, fmt.Errorf("storage release allocation id is required")
	}
	if nodeID == "" {
		return nil, fmt.Errorf("storage release node id is required")
	}
	if err := c.store.ReleaseVolumeBindings(ctx, allocationID, nodeID); err != nil {
		return nil, err
	}
	return &privatestoragev1.ReleaseVolumeBindingResponse{}, nil
}

func (c *Controller) RetryFailedVolumeBinding(ctx context.Context, req *privatestoragev1.RetryFailedVolumeBindingRequest) (*privatestoragev1.RetryFailedVolumeBindingResponse, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	bindingID := strings.TrimSpace(req.GetBindingID())
	if bindingID == "" {
		return nil, fmt.Errorf("storage retry binding id is required")
	}
	binding, err := c.store.RetryFailedVolumeBinding(ctx, bindingID, req.GetOperatorReason())
	if err != nil {
		return nil, err
	}
	return &privatestoragev1.RetryFailedVolumeBindingResponse{Binding: binding}, nil
}

func (c *Controller) ListVolumeBindings(ctx context.Context, req *privatestoragev1.ListVolumeBindingsRequest) (*privatestoragev1.ListVolumeBindingsResponse, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	filter := kernel.NormalizeVolumeBindingListFilter(volumeBindingListFilterFromProto(req))
	if err := kernel.ValidateVolumeBindingListFilter(filter); err != nil {
		return nil, err
	}
	bindings, err := c.store.ListVolumeBindings(ctx, filter)
	if err != nil {
		return nil, err
	}
	return &privatestoragev1.ListVolumeBindingsResponse{Bindings: bindings}, nil
}

func (c *Controller) ReportVolumePublish(ctx context.Context, req *privatestoragev1.ReportVolumePublishRequest) (*privatestoragev1.ReportVolumePublishResponse, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	allocationID := strings.TrimSpace(req.GetAllocationID())
	nodeID := strings.TrimSpace(req.GetNodeID())
	if allocationID == "" {
		return nil, fmt.Errorf("storage publish report allocation id is required")
	}
	if nodeID == "" {
		return nil, fmt.Errorf("storage publish report node id is required")
	}
	if len(req.GetObservations()) == 0 {
		return nil, fmt.Errorf("storage publish report observations are required")
	}
	for _, observation := range req.GetObservations() {
		if observation == nil {
			return nil, fmt.Errorf("storage publish report observation is required")
		}
		if err := kernel.ValidateVolumePublishObservation(observation); err != nil {
			return nil, err
		}
	}
	if err := c.store.ReportVolumePublish(ctx, allocationID, nodeID, req.GetObservations()); err != nil {
		return nil, err
	}
	return &privatestoragev1.ReportVolumePublishResponse{}, nil
}

func (c *Controller) ReportVolumeRelease(ctx context.Context, req *privatestoragev1.ReportVolumeReleaseRequest) (*privatestoragev1.ReportVolumeReleaseResponse, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	allocationID := strings.TrimSpace(req.GetAllocationID())
	nodeID := strings.TrimSpace(req.GetNodeID())
	if allocationID == "" {
		return nil, fmt.Errorf("storage release report allocation id is required")
	}
	if nodeID == "" {
		return nil, fmt.Errorf("storage release report node id is required")
	}
	for _, observation := range req.GetObservations() {
		if observation == nil {
			return nil, fmt.Errorf("storage release report observation is required")
		}
		if err := kernel.ValidateVolumeReleaseObservation(observation); err != nil {
			return nil, err
		}
	}
	if err := c.store.ReportVolumeRelease(ctx, allocationID, nodeID, req.GetObservations()); err != nil {
		return nil, err
	}
	return &privatestoragev1.ReportVolumeReleaseResponse{}, nil
}

func volumeBindingListFilterFromProto(req *privatestoragev1.ListVolumeBindingsRequest) kernel.VolumeBindingListFilter {
	if req == nil {
		return kernel.VolumeBindingListFilter{}
	}
	filter := req.GetFilter()
	return kernel.VolumeBindingListFilter{
		Statuses:     append([]storagev1.VolumeStatus(nil), filter.GetStatuses()...),
		Namespace:    filter.GetNamespace(),
		ClaimName:    filter.GetClaimName(),
		WorkloadID:   filter.GetWorkloadID(),
		AllocationID: filter.GetAllocationID(),
		NodeID:       filter.GetNodeID(),
		Limit:        int(req.GetLimit()),
	}
}
