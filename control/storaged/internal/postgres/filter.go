package postgres

import storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"

func claimMatchesFilter(claim *storagev1.VolumeClaim, filter *storagev1.VolumeClaimListFilter) bool {
	if filter == nil {
		return claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED
	}
	if filter.GetNamespace() != "" && claim.GetNamespace() != filter.GetNamespace() {
		return false
	}
	for _, status := range filter.GetStatuses() {
		if claim.GetStatus() == status {
			goto labels
		}
	}
	if len(filter.GetStatuses()) > 0 {
		return false
	}
	if claim.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		return false
	}
labels:
	for key, value := range filter.GetLabels() {
		if claim.GetLabels()[key] != value {
			return false
		}
	}
	return true
}
