package storage

import (
	"fmt"
	"sort"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

type HealthConsistencyCounts struct {
	InconsistentClaims int64
	InvalidBindings    int64
}

type ClaimHealthState struct {
	ClaimID string
	Status  storagev1.VolumeStatus
}

func BuildVolumeBindingHealth(bindingCounts, claimCounts map[storagev1.VolumeStatus]int64, stuckReleasingBindings int64, consistency HealthConsistencyCounts) *privatestoragev1.VolumeBindingHealth {
	health := &privatestoragev1.VolumeBindingHealth{
		BindingStatusCounts:    volumeStatusCounts(bindingCounts),
		ClaimStatusCounts:      volumeStatusCounts(claimCounts),
		StuckReleasingBindings: stuckReleasingBindings,
		InconsistentClaims:     consistency.InconsistentClaims,
		InvalidBindings:        consistency.InvalidBindings,
	}
	health.DeletingClaims = claimCounts[storagev1.VolumeStatus_VOLUME_STATUS_DELETING]
	for status, count := range bindingCounts {
		health.TotalBindings += count
		if BindingStatusIsActive(status) {
			health.ActiveBindings += count
		}
		switch status {
		case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:
			health.PublishedBindings += count
		case storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
			health.FailedBindings += count
		case storagev1.VolumeStatus_VOLUME_STATUS_RELEASING:
			health.ReleasingBindings += count
		case storagev1.VolumeStatus_VOLUME_STATUS_DELETED:
			health.DeletedBindings += count
		}
	}
	return health
}

func EvaluateHealthConsistency(claims []ClaimHealthState, bindings []*privatestoragev1.VolumeBinding) HealthConsistencyCounts {
	bindingsByClaim := map[string][]*privatestoragev1.VolumeBinding{}
	var out HealthConsistencyCounts
	for _, binding := range bindings {
		if binding == nil {
			out.InvalidBindings++
			continue
		}
		if !bindingStatusPayloadIsValid(binding) {
			out.InvalidBindings++
		}
		if binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			continue
		}
		bindingsByClaim[binding.GetClaimID()] = append(bindingsByClaim[binding.GetClaimID()], binding)
	}
	for _, claim := range claims {
		if claim.ClaimID == "" || claim.Status == storagev1.VolumeStatus_VOLUME_STATUS_DELETING || claim.Status == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			continue
		}
		expected := aggregateClaimStatus(bindingsByClaim[claim.ClaimID])
		if claim.Status != expected {
			out.InconsistentClaims++
		}
	}
	return out
}

func bindingStatusPayloadIsValid(binding *privatestoragev1.VolumeBinding) bool {
	switch binding.GetStatus() {
	case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:
		return binding.GetPublishedVolume() != nil && binding.GetPublishedAt() != nil
	case storagev1.VolumeStatus_VOLUME_STATUS_DELETED:
		return binding.GetReleasedAt() != nil
	case storagev1.VolumeStatus_VOLUME_STATUS_BOUND,
		storagev1.VolumeStatus_VOLUME_STATUS_RELEASING,
		storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
		return true
	default:
		return false
	}
}

func aggregateClaimStatus(bindings []*privatestoragev1.VolumeBinding) storagev1.VolumeStatus {
	if len(bindings) == 0 {
		return storagev1.VolumeStatus_VOLUME_STATUS_BOUND
	}
	out := storagev1.VolumeStatus_VOLUME_STATUS_BOUND
	for _, binding := range bindings {
		switch binding.GetStatus() {
		case storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
			return storagev1.VolumeStatus_VOLUME_STATUS_FAILED
		case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:
			out = storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED
		}
	}
	return out
}

func BindingStatusIsActive(status storagev1.VolumeStatus) bool {
	switch status {
	case storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED,
		storagev1.VolumeStatus_VOLUME_STATUS_DELETED:
		return false
	default:
		return true
	}
}

func ValidPublishObservationStatus(status storagev1.VolumeStatus) bool {
	switch status {
	case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
		storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
		return true
	default:
		return false
	}
}

func ValidReleaseObservationStatus(status storagev1.VolumeStatus) bool {
	switch status {
	case storagev1.VolumeStatus_VOLUME_STATUS_DELETED,
		storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
		return true
	default:
		return false
	}
}

func ValidateVolumePublishObservation(observation *privatestoragev1.VolumePublishObservation) error {
	if observation == nil {
		return nil
	}
	if strings.TrimSpace(observation.GetBindingID()) == "" {
		return fmt.Errorf("volume publish observation binding id is required")
	}
	if !ValidPublishObservationStatus(observation.GetStatus()) {
		return fmt.Errorf("volume publish observation status %s is invalid", observation.GetStatus())
	}
	switch observation.GetStatus() {
	case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:
		if observation.GetPublishedVolume() == nil {
			return fmt.Errorf("published volume is required")
		}
	case storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
		if strings.TrimSpace(observation.GetMessage()) == "" {
			return fmt.Errorf("volume publish failure message is required")
		}
	}
	return nil
}

func ValidateVolumeReleaseObservation(observation *privatestoragev1.VolumeReleaseObservation) error {
	if observation == nil {
		return nil
	}
	if strings.TrimSpace(observation.GetBindingID()) == "" {
		return fmt.Errorf("volume release observation binding id is required")
	}
	if !ValidReleaseObservationStatus(observation.GetStatus()) {
		return fmt.Errorf("volume release observation status %s is invalid", observation.GetStatus())
	}
	if observation.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_FAILED && strings.TrimSpace(observation.GetMessage()) == "" {
		return fmt.Errorf("volume release failure message is required")
	}
	return nil
}

func volumeStatusCounts(in map[storagev1.VolumeStatus]int64) []*privatestoragev1.VolumeStatusCount {
	out := make([]*privatestoragev1.VolumeStatusCount, 0, len(in))
	for status, count := range in {
		out = append(out, &privatestoragev1.VolumeStatusCount{Status: status, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].GetStatus() < out[j].GetStatus()
	})
	return out
}
