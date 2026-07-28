package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
)

func (w *HttpWorker) ReconcileMountLeases(ctx context.Context, req *ReconcileMountLeasesRequest) (*ReconcileMountLeasesResponse, error) {
	if w.mountStore == nil {
		return nil, fmt.Errorf("mount store is not initialized")
	}
	owner := req.Owner
	if owner == "" {
		return nil, fmt.Errorf("owner is required")
	}
	if err := validateOwner(owner); err != nil {
		return nil, err
	}
	desired := make(map[string]struct{}, len(req.LeaseIDs))
	for _, leaseID := range req.LeaseIDs {
		if err := validateLeaseID(leaseID); err != nil {
			return nil, err
		}
		desired[leaseID] = struct{}{}
	}
	leases, err := w.mountStore.ListLeases()
	if err != nil {
		return nil, err
	}
	response := &ReconcileMountLeasesResponse{}
	var reconcileErr error
	for _, lease := range leases {
		if lease.Owner != owner {
			continue
		}
		if _, ok := desired[lease.ID]; ok {
			unlock := w.lockMount(lease.MountKey)
			err := w.mountStore.RetainLease(lease.ID, owner)
			unlock()
			if err != nil {
				reconcileErr = errors.Join(reconcileErr, fmt.Errorf("retain desired lease %s: %w", lease.ID, err))
				continue
			}
			response.Retained++
			continue
		}
		response.Releasing++
		if err := w.releaseLease(lease.ID, func(record *mountstore.Record) error { return w.unmountResource(ctx, record) }); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("release stale lease %s: %w", lease.ID, err))
		}
	}
	return response, reconcileErr
}

func validateLeaseID(leaseID string) error {
	trimmed := strings.TrimSpace(leaseID)
	if trimmed == "" {
		return fmt.Errorf("lease_id is required")
	}
	if trimmed != leaseID {
		return fmt.Errorf("lease_id must not have surrounding whitespace")
	}
	if len(leaseID) > 256 {
		return fmt.Errorf("lease_id exceeds 256 bytes")
	}
	return nil
}

func validateOwner(owner string) error {
	if owner == "" {
		return fmt.Errorf("owner is required")
	}
	if strings.TrimSpace(owner) != owner {
		return fmt.Errorf("owner must not have surrounding whitespace")
	}
	if len(owner) > 128 {
		return fmt.Errorf("owner exceeds 128 bytes")
	}
	return nil
}

func (w *HttpWorker) lockMount(key string) func() {
	w.mountLocksMu.Lock()
	entry := w.mountLocks[key]
	if entry == nil {
		entry = &mountLock{}
		w.mountLocks[key] = entry
	}
	entry.refs++
	w.mountLocksMu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		w.mountLocksMu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(w.mountLocks, key)
		}
		w.mountLocksMu.Unlock()
	}
}

func (w *HttpWorker) existingLease(leaseID string) (*mountstore.Lease, *mountstore.Record, error) {
	lease, err := w.mountStore.GetLease(leaseID)
	if err != nil || lease == nil {
		return lease, nil, err
	}
	record, err := w.mountStore.GetMount(lease.MountKey)
	if err != nil {
		return nil, nil, err
	}
	if record == nil {
		return nil, nil, fmt.Errorf("lease %q references missing mount %q", leaseID, lease.MountKey)
	}
	return lease, record, nil
}

func (w *HttpWorker) releaseLease(leaseID string, unmount func(*mountstore.Record) error) error {
	if err := validateLeaseID(leaseID); err != nil {
		return err
	}
	lease, record, err := w.existingLease(leaseID)
	if err != nil || lease == nil {
		return err
	}
	unlock := w.lockMount(lease.MountKey)
	defer unlock()

	lease, record, err = w.existingLease(leaseID)
	if err != nil || lease == nil {
		return err
	}
	count, err := w.mountStore.LeaseCount(lease.MountKey)
	if err != nil {
		return err
	}
	if count > 1 {
		return w.mountStore.ReleaseLease(leaseID, false)
	}
	if err := w.mountStore.BeginRelease(leaseID); err != nil {
		return err
	}
	if err := unmount(record); err != nil {
		_ = w.mountStore.RecordReleaseFailure(leaseID, err)
		return err
	}
	return w.mountStore.ReleaseLease(leaseID, true)
}

func (w *HttpWorker) runMountReleaseReconciler(ctx context.Context) {
	w.reconcileReleasingMounts(ctx)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.reconcileReleasingMounts(ctx)
		}
	}
}

func (w *HttpWorker) reconcileReleasingMounts(ctx context.Context) {
	leases, err := w.mountStore.ListReleasing()
	if err != nil {
		return
	}
	for _, candidate := range leases {
		if err := ctx.Err(); err != nil {
			return
		}
		lease, record, err := w.existingLease(candidate.ID)
		if err != nil || lease == nil || record == nil {
			continue
		}
		unlock := w.lockMount(lease.MountKey)
		count, countErr := w.mountStore.LeaseCount(lease.MountKey)
		if countErr == nil && count == 1 {
			if err := w.mountStore.BeginRelease(lease.ID); err == nil {
				if err := w.unmountResource(ctx, record); err != nil {
					_ = w.mountStore.RecordReleaseFailure(lease.ID, err)
				} else {
					_ = w.mountStore.ReleaseLease(lease.ID, true)
				}
			}
		}
		unlock()
	}
}
