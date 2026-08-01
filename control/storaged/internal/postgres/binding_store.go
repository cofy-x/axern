package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) GetVolumeBindingForClaim(ctx context.Context, claimID string) (*privatestoragev1.VolumeBinding, bool, error) {
	row := s.db.Pool().QueryRow(ctx, `
		SELECT payload FROM storage_volume_bindings
		WHERE claim_id = $1
		  AND status <> $2
		ORDER BY created_at
		LIMIT 1
	`, strings.TrimSpace(claimID), storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String())
	out, err := scanVolumeBinding(row)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (s *Store) ReserveVolumeBinding(ctx context.Context, req kernel.VolumeBindingReserve) (*privatestoragev1.ResolvedNodeVolume, error) {
	if req.Claim == nil || req.Class == nil || req.Mount == nil {
		return nil, fmt.Errorf("volume binding reserve requires claim, class, and mount")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	lockedClaim, err := scanVolumeClaim(tx.QueryRow(ctx, `
		SELECT payload FROM storage_volume_claims
		WHERE claim_id = $1
		FOR UPDATE
	`, req.Claim.GetID()))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("volume claim %q not found", req.Claim.GetID())
		}
		return nil, err
	}
	if lockedClaim.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		return nil, fmt.Errorf("volume claim %q not found", req.Claim.GetID())
	}
	if err := kernel.ValidateVolumeClaimOwnership(lockedClaim, req.WorkloadID, req.WorkloadType); err != nil {
		return nil, err
	}
	if boundNode := lockedClaim.GetTopology().GetNodeID(); boundNode != "" && boundNode != req.NodeID {
		return nil, fmt.Errorf("volume claim %q is already bound to node %q", req.Claim.GetID(), boundNode)
	}
	req.Claim = lockedClaim
	volume := resolvedNodeVolume(req)
	existingSameBinding, err := scanVolumeBinding(tx.QueryRow(ctx, `
		SELECT payload FROM storage_volume_bindings
		WHERE binding_id = $1
		  AND status <> $2
		FOR UPDATE
	`, volume.GetBindingID(), storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String()))
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	if err == nil {
		if existingSameBinding.GetClaimID() != req.Claim.GetID() || existingSameBinding.GetAllocationID() != req.AllocationID || existingSameBinding.GetNodeID() != req.NodeID {
			return nil, fmt.Errorf("volume binding %q already exists for a different allocation target", volume.GetBindingID())
		}
		switch existingSameBinding.GetStatus() {
		case storagev1.VolumeStatus_VOLUME_STATUS_BOUND, storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:
			if !resolvedNodeVolumesEqual(existingSameBinding.GetResolvedVolume(), volume) {
				return nil, fmt.Errorf("volume binding %q already exists with a different resolved volume", volume.GetBindingID())
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return proto.Clone(existingSameBinding.GetResolvedVolume()).(*privatestoragev1.ResolvedNodeVolume), nil
		case storagev1.VolumeStatus_VOLUME_STATUS_RELEASING:
			return nil, fmt.Errorf("volume binding %q is releasing", volume.GetBindingID())
		}
	}
	existing, err := scanVolumeBinding(tx.QueryRow(ctx, `
		SELECT payload FROM storage_volume_bindings
		WHERE claim_id = $1
		  AND status <> $2
		ORDER BY created_at
		LIMIT 1
	`, req.Claim.GetID(), storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String()))
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	if err == nil && existing.GetResolvedVolume().GetTopology().GetNodeID() != "" && existing.GetResolvedVolume().GetTopology().GetNodeID() != req.NodeID {
		return nil, fmt.Errorf("volume claim %q is already bound to node %q", req.Claim.GetID(), existing.GetResolvedVolume().GetTopology().GetNodeID())
	}
	now := time.Now().UTC()
	binding := &privatestoragev1.VolumeBinding{
		BindingID:      volume.GetBindingID(),
		ClaimID:        volume.GetClaimID(),
		Namespace:      req.Namespace,
		ClaimName:      req.Claim.GetName(),
		WorkloadID:     req.WorkloadID,
		WorkloadType:   req.WorkloadType,
		AllocationID:   req.AllocationID,
		NodeID:         req.NodeID,
		Status:         storagev1.VolumeStatus_VOLUME_STATUS_BOUND,
		ResolvedVolume: volume,
		CreatedAt:      timestamppb.New(now),
		UpdatedAt:      timestamppb.New(now),
	}
	payload, err := marshalProto(binding)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO storage_volume_bindings (
			binding_id, claim_id, namespace, claim_name, workload_id, workload_type, allocation_id, node_id, status, payload, message, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13)
		ON CONFLICT (binding_id) DO UPDATE
		SET status = EXCLUDED.status,
		    payload = EXCLUDED.payload,
		    message = EXCLUDED.message,
		    published_at = NULL,
		    released_at = NULL,
		    updated_at = EXCLUDED.updated_at
	`, binding.GetBindingID(), binding.GetClaimID(), binding.GetNamespace(), binding.GetClaimName(), binding.GetWorkloadID(), binding.GetWorkloadType(), binding.GetAllocationID(), binding.GetNodeID(), binding.GetStatus().String(), payload, binding.GetMessage(), now, now); err != nil {
		return nil, err
	}
	// Persist from the row locked in this transaction, never from the caller's
	// pre-lock snapshot. The request may have been resolved before an ownership
	// or status transition committed.
	claim := proto.Clone(lockedClaim).(*storagev1.VolumeClaim)
	if err := kernel.TransitionClaimStatus(claim.GetStatus(), storagev1.VolumeStatus_VOLUME_STATUS_BOUND); err != nil {
		return nil, err
	}
	claim.Status = storagev1.VolumeStatus_VOLUME_STATUS_BOUND
	claim.Topology = &storagev1.VolumeTopology{NodeID: req.NodeID}
	claim.Version++
	claim.UpdatedAt = timestamppb.New(now)
	if err := s.updateVolumeClaim(ctx, tx, claim); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return volume, nil
}

func (s *Store) ReleaseVolumeBindings(ctx context.Context, allocationID, nodeID string) error {
	allocationID = strings.TrimSpace(allocationID)
	nodeID = strings.TrimSpace(nodeID)
	if allocationID == "" {
		return fmt.Errorf("allocation id is required")
	}
	if nodeID == "" {
		return fmt.Errorf("node id is required")
	}
	return s.ReportVolumeRelease(ctx, allocationID, nodeID, nil)
}

func (s *Store) RetryFailedVolumeBinding(ctx context.Context, bindingID, operatorReason string) (*privatestoragev1.VolumeBinding, error) {
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return nil, fmt.Errorf("volume binding id is required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	binding, err := s.lockVolumeBindingByID(ctx, tx, bindingID)
	if err != nil {
		return nil, err
	}
	if binding.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_FAILED {
		return nil, fmt.Errorf("volume binding %q is %s, want failed", bindingID, binding.GetStatus())
	}
	now := time.Now().UTC()
	next := proto.Clone(binding).(*privatestoragev1.VolumeBinding)
	next.Status = storagev1.VolumeStatus_VOLUME_STATUS_BOUND
	next.Message = strings.TrimSpace(operatorReason)
	next.PublishedVolume = nil
	next.PublishedAt = nil
	next.ReleasedAt = nil
	next.UpdatedAt = timestamppb.New(now)
	if err := s.updateVolumeBinding(ctx, tx, next); err != nil {
		return nil, err
	}
	if err := s.recomputeClaimStatus(ctx, tx, next.GetClaimID(), now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return proto.Clone(next).(*privatestoragev1.VolumeBinding), nil
}

func (s *Store) ListVolumeBindings(ctx context.Context, filter kernel.VolumeBindingListFilter) ([]*privatestoragev1.VolumeBinding, error) {
	filter = kernel.NormalizeVolumeBindingListFilter(filter)
	if err := kernel.ValidateVolumeBindingListFilter(filter); err != nil {
		return nil, err
	}
	query := `
		SELECT payload FROM storage_volume_bindings
		WHERE true
	`
	args := make([]any, 0, 7)
	nextArg := 1
	if len(filter.Statuses) > 0 {
		statuses := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			statuses = append(statuses, status.String())
		}
		query += fmt.Sprintf(" AND status = ANY($%d)", nextArg)
		args = append(args, statuses)
		nextArg++
	} else {
		query += fmt.Sprintf(" AND status <> $%d", nextArg)
		args = append(args, storagev1.VolumeStatus_VOLUME_STATUS_DELETED.String())
		nextArg++
	}
	if filter.Namespace != "" {
		query += fmt.Sprintf(" AND namespace = $%d", nextArg)
		args = append(args, filter.Namespace)
		nextArg++
	}
	if filter.ClaimName != "" {
		query += fmt.Sprintf(" AND claim_name = $%d", nextArg)
		args = append(args, filter.ClaimName)
		nextArg++
	}
	if filter.WorkloadID != "" {
		query += fmt.Sprintf(" AND workload_id = $%d", nextArg)
		args = append(args, filter.WorkloadID)
		nextArg++
	}
	if filter.AllocationID != "" {
		query += fmt.Sprintf(" AND allocation_id = $%d", nextArg)
		args = append(args, filter.AllocationID)
		nextArg++
	}
	if filter.NodeID != "" {
		query += fmt.Sprintf(" AND node_id = $%d", nextArg)
		args = append(args, filter.NodeID)
		nextArg++
	}
	query += fmt.Sprintf(" ORDER BY updated_at DESC, binding_id ASC LIMIT $%d", nextArg)
	args = append(args, filter.Limit)
	rows, err := s.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*privatestoragev1.VolumeBinding, 0)
	for rows.Next() {
		binding, err := scanVolumeBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func (s *Store) ReportVolumePublish(ctx context.Context, allocationID, nodeID string, observations []*privatestoragev1.VolumePublishObservation) error {
	allocationID = strings.TrimSpace(allocationID)
	nodeID = strings.TrimSpace(nodeID)
	if allocationID == "" {
		return fmt.Errorf("allocation id is required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	for _, observation := range observations {
		if observation == nil {
			continue
		}
		if err := kernel.ValidateVolumePublishObservation(observation); err != nil {
			return err
		}
		binding, err := s.lockVolumeBinding(ctx, tx, allocationID, nodeID, observation.GetBindingID(), true)
		if err != nil {
			return err
		}
		next := proto.Clone(binding).(*privatestoragev1.VolumeBinding)
		next.Status = observation.GetStatus()
		next.Message = strings.TrimSpace(observation.GetMessage())
		next.UpdatedAt = timestamppb.New(now)
		if observation.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED {
			next.PublishedVolume = proto.Clone(observation.GetPublishedVolume()).(*privatestoragev1.PublishedNodeVolume)
			next.PublishedAt = timestamppb.New(now)
		} else {
			next.PublishedVolume = nil
			next.PublishedAt = nil
		}
		if err := s.updateVolumeBinding(ctx, tx, next); err != nil {
			return err
		}
		if err := s.updateClaimFromBindingStatus(ctx, tx, next.GetClaimID(), next.GetStatus(), next.GetResolvedVolume().GetTopology(), next.GetMessage(), now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ReportVolumeRelease(ctx context.Context, allocationID, nodeID string, observations []*privatestoragev1.VolumeReleaseObservation) error {
	allocationID = strings.TrimSpace(allocationID)
	nodeID = strings.TrimSpace(nodeID)
	if allocationID == "" {
		return fmt.Errorf("allocation id is required")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	bindings, err := s.releaseTargetBindings(ctx, tx, allocationID, nodeID, observations)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			if releaseObservationStatus(binding.GetBindingID(), observations) == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
				continue
			}
			return fmt.Errorf("volume binding %q is already deleted", binding.GetBindingID())
		}
		next := proto.Clone(binding).(*privatestoragev1.VolumeBinding)
		next.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETED
		next.Message = ""
		next.ReleasedAt = timestamppb.New(now)
		next.UpdatedAt = timestamppb.New(now)
		for _, observation := range observations {
			if observation != nil && observation.GetBindingID() == binding.GetBindingID() {
				next.Status = observation.GetStatus()
				next.Message = strings.TrimSpace(observation.GetMessage())
				break
			}
		}
		if next.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_FAILED {
			next.ReleasedAt = nil
		}
		if err := s.updateVolumeBinding(ctx, tx, next); err != nil {
			return err
		}
		if err := s.recomputeClaimStatus(ctx, tx, next.GetClaimID(), now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func releaseObservationStatus(bindingID string, observations []*privatestoragev1.VolumeReleaseObservation) storagev1.VolumeStatus {
	for _, observation := range observations {
		if observation != nil && observation.GetBindingID() == bindingID {
			return observation.GetStatus()
		}
	}
	return storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED
}
