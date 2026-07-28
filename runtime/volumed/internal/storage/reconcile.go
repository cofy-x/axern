package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type InvalidPublishedVolumeError struct {
	Reason string
}

func (e InvalidPublishedVolumeError) Error() string {
	if e.Reason == "" {
		return "published volume is invalid"
	}
	return e.Reason
}

type VolumeHealth struct {
	LastReconcileAt     time.Time
	LastReconcileError  string
	LastReconcileResult ReconcileResult
}

// ReconcileResult separates observed state from completed cleanup work.
// StaleAllocationCount and InvalidVolumeCount are records found during the
// pass; UnpublishedCount is incremented only after provider unpublish succeeds
// and the in-memory publish record is removed.
type ReconcileResult struct {
	ActiveAllocationCount int
	RetainedCount         int
	UnpublishedCount      int
	StaleAllocationCount  int
	InvalidVolumeCount    int
}

func (m *Manager) Reconcile(ctx context.Context, activeAllocationIDs []string) (result ReconcileResult, err error) {
	if m == nil {
		return ReconcileResult{}, nil
	}
	defer func() {
		m.recordReconcileHealth(time.Now().UTC(), result, err)
	}()
	active := make(map[string]struct{}, len(activeAllocationIDs))
	for _, id := range activeAllocationIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			active[id] = struct{}{}
		}
	}
	result.ActiveAllocationCount = len(active)
	m.mu.Lock()
	published := clonePublishedMap(m.published)
	m.mu.Unlock()

	removed := map[string][]*runtimevolumev1.PublishedVolume{}
	var firstValidationErr error
	for _, allocationID := range sortedPublishedAllocationIDs(published) {
		items := published[allocationID]
		if _, ok := active[allocationID]; ok {
			for _, item := range items {
				if err := m.validatePublished(ctx, allocationID, item); err != nil {
					if isInvalidPublishedVolume(err) {
						removed[allocationID] = append(removed[allocationID], clonePublished(item))
						result.InvalidVolumeCount++
						continue
					}
					if firstValidationErr == nil {
						firstValidationErr = err
					}
					continue
				}
				result.RetainedCount++
			}
			continue
		}
		result.StaleAllocationCount++
		removed[allocationID] = clonePublishedSlice(items)
	}
	if firstValidationErr != nil {
		return result, firstValidationErr
	}

	var firstErr error
	successfullyUnpublished := map[string][]*runtimevolumev1.PublishedVolume{}
	for _, allocationID := range sortedPublishedAllocationIDs(removed) {
		items := removed[allocationID]
		for _, item := range items {
			if err := m.unpublishOne(ctx, allocationID, item); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			successfullyUnpublished[allocationID] = append(successfullyUnpublished[allocationID], item)
		}
	}
	m.mu.Lock()
	for _, allocationID := range sortedPublishedAllocationIDs(successfullyUnpublished) {
		for _, item := range successfullyUnpublished[allocationID] {
			if removePublishedIfEqual(m.published, allocationID, item) {
				result.UnpublishedCount++
			}
		}
	}
	if result.UnpublishedCount > 0 {
		if saveErr := m.saveLocked(ctx); saveErr != nil && firstErr == nil {
			firstErr = saveErr
		}
	}
	m.mu.Unlock()
	return result, firstErr
}

func (m *Manager) Health() *runtimevolumev1.VolumeManagerHealth {
	if m == nil {
		return &runtimevolumev1.VolumeManagerHealth{Status: runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_DISABLED}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	health := &runtimevolumev1.VolumeManagerHealth{
		Status:                             runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_OK,
		PublishedVolumeCount:               int32(countPublished(m.published)),
		LastReconcileError:                 m.health.LastReconcileError,
		LastReconcileRetainedCount:         int32(m.health.LastReconcileResult.RetainedCount),
		LastReconcileUnpublishedCount:      int32(m.health.LastReconcileResult.UnpublishedCount),
		LastReconcileActiveAllocationCount: int32(m.health.LastReconcileResult.ActiveAllocationCount),
		LastReconcileStaleAllocationCount:  int32(m.health.LastReconcileResult.StaleAllocationCount),
		LastReconcileInvalidVolumeCount:    int32(m.health.LastReconcileResult.InvalidVolumeCount),
	}
	if !m.health.LastReconcileAt.IsZero() {
		health.LastReconcileAt = timestamppb.New(m.health.LastReconcileAt)
	}
	if strings.TrimSpace(health.GetLastReconcileError()) != "" {
		health.Status = runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_ERROR
	}
	return health
}

func (m *Manager) recordReconcileHealth(at time.Time, result ReconcileResult, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health.LastReconcileAt = at
	m.health.LastReconcileResult = result
	if err != nil {
		m.health.LastReconcileError = err.Error()
	} else {
		m.health.LastReconcileError = ""
	}
}

func (m *Manager) validatePublished(ctx context.Context, allocationID string, item *runtimevolumev1.PublishedVolume) error {
	if item == nil {
		return InvalidPublishedVolumeError{Reason: "published volume is required"}
	}
	if err := validatePublishedRecord(allocationID, item); err != nil {
		return InvalidPublishedVolumeError{Reason: err.Error()}
	}
	provider := m.providers[item.GetBackend()]
	if provider == nil {
		return fmt.Errorf("volume backend %s is not supported", item.GetBackend())
	}
	return provider.ValidatePublished(ctx, allocationID, item)
}

func isInvalidPublishedVolume(err error) bool {
	var invalid InvalidPublishedVolumeError
	return errors.As(err, &invalid)
}
