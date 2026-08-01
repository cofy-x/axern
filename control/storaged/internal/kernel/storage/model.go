package storage

import (
	"fmt"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

const DefaultNamespace = "default"

type VolumeClassCreate struct {
	Name                 string
	Backend              storagev1.VolumeBackend
	AccessModes          []storagev1.VolumeAccessMode
	DefaultReclaimPolicy storagev1.VolumeReclaimPolicy
	ConsistencyProfile   storagev1.VolumeConsistencyProfile
	RuntimeCompatibility *storagev1.VolumeRuntimeCompatibility
	Parameters           map[string]string
}

type VolumeClaimCreate struct {
	Namespace         string
	Name              string
	ClassName         string
	RequestedCapacity int64
	AccessMode        storagev1.VolumeAccessMode
	ReclaimPolicy     storagev1.VolumeReclaimPolicy
	BindingScope      storagev1.VolumeBindingScope
	Parameters        map[string]string
	Labels            map[string]string
}

type VolumeBindingReserve struct {
	Namespace    string
	WorkloadID   string
	WorkloadType string
	AllocationID string
	NodeID       string
	Mount        *privatestoragev1.WorkloadVolumeMount
	Claim        *storagev1.VolumeClaim
	Class        *storagev1.VolumeClass
}

const (
	DefaultVolumeBindingListLimit = 50
	MaxVolumeBindingListLimit     = 200
)

type VolumeBindingListFilter struct {
	Statuses     []storagev1.VolumeStatus
	Namespace    string
	ClaimName    string
	WorkloadID   string
	AllocationID string
	NodeID       string
	Limit        int
}

func NormalizeVolumeBindingListFilter(in VolumeBindingListFilter) VolumeBindingListFilter {
	out := VolumeBindingListFilter{
		Statuses:     append([]storagev1.VolumeStatus(nil), in.Statuses...),
		Namespace:    strings.TrimSpace(in.Namespace),
		ClaimName:    strings.TrimSpace(in.ClaimName),
		WorkloadID:   strings.TrimSpace(in.WorkloadID),
		AllocationID: strings.TrimSpace(in.AllocationID),
		NodeID:       strings.TrimSpace(in.NodeID),
		Limit:        in.Limit,
	}
	if out.Limit <= 0 {
		out.Limit = DefaultVolumeBindingListLimit
	}
	if out.Limit > MaxVolumeBindingListLimit {
		out.Limit = MaxVolumeBindingListLimit
	}
	return out
}

func ValidateVolumeBindingListFilter(in VolumeBindingListFilter) error {
	seen := map[storagev1.VolumeStatus]struct{}{}
	for _, status := range in.Statuses {
		if status == storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED {
			return fmt.Errorf("volume binding status filter is invalid")
		}
		if _, ok := seen[status]; ok {
			return fmt.Errorf("volume binding status filter %s is duplicated", status)
		}
		seen[status] = struct{}{}
	}
	if in.Limit <= 0 {
		return fmt.Errorf("volume binding list limit must be positive")
	}
	if in.Limit > MaxVolumeBindingListLimit {
		return fmt.Errorf("volume binding list limit must be at most %d", MaxVolumeBindingListLimit)
	}
	return nil
}

func ValidateVolumeClassCreate(in VolumeClassCreate) error {
	if !StableName(in.Name) {
		return fmt.Errorf("volume class name is required and may only contain letters, digits, '.', '_', or '-'")
	}
	if in.Backend == storagev1.VolumeBackend_VOLUME_BACKEND_UNSPECIFIED {
		return fmt.Errorf("volume class backend is required")
	}
	if len(in.AccessModes) == 0 {
		return fmt.Errorf("volume class access modes are required")
	}
	seen := map[storagev1.VolumeAccessMode]struct{}{}
	for _, mode := range in.AccessModes {
		if mode == storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_UNSPECIFIED {
			return fmt.Errorf("volume class access mode is required")
		}
		if _, ok := seen[mode]; ok {
			return fmt.Errorf("volume class access mode %s is duplicated", mode)
		}
		seen[mode] = struct{}{}
	}
	if in.DefaultReclaimPolicy == storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_UNSPECIFIED {
		return fmt.Errorf("volume class default reclaim policy is required")
	}
	if in.ConsistencyProfile == storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_UNSPECIFIED {
		return fmt.Errorf("volume class consistency profile is required")
	}
	if in.RuntimeCompatibility == nil || (!in.RuntimeCompatibility.GetSupportsRunc() && !in.RuntimeCompatibility.GetSupportsRunsc()) {
		return fmt.Errorf("volume class runtime compatibility must support at least one runtime")
	}
	return nil
}

func ValidateVolumeClaimCreate(in VolumeClaimCreate, class *storagev1.VolumeClass) error {
	if !StableName(NormalizeNamespace(in.Namespace)) {
		return fmt.Errorf("volume claim namespace is invalid")
	}
	if !StableName(in.Name) {
		return fmt.Errorf("volume claim name is required and may only contain letters, digits, '.', '_', or '-'")
	}
	if !StableName(in.ClassName) {
		return fmt.Errorf("volume claim class name is required")
	}
	if in.RequestedCapacity < 0 {
		return fmt.Errorf("volume claim requested capacity must be non-negative")
	}
	if in.AccessMode == storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_UNSPECIFIED {
		return fmt.Errorf("volume claim access mode is required")
	}
	if in.ReclaimPolicy == storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_UNSPECIFIED {
		return fmt.Errorf("volume claim reclaim policy is required")
	}
	if in.BindingScope == storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_UNSPECIFIED {
		return fmt.Errorf("volume claim binding scope is required")
	}
	if class != nil && !ClassSupportsAccessMode(class, in.AccessMode) {
		return fmt.Errorf("volume class %q does not support access mode %s", class.GetName(), in.AccessMode)
	}
	return nil
}

func NormalizeNamespace(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultNamespace
	}
	return value
}

func StableName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func ClassSupportsAccessMode(class *storagev1.VolumeClass, mode storagev1.VolumeAccessMode) bool {
	if class == nil {
		return false
	}
	for _, supported := range class.GetAccessModes() {
		if supported == mode {
			return true
		}
	}
	return false
}

func IsTerminalClaimStatus(status storagev1.VolumeStatus) bool {
	return status == storagev1.VolumeStatus_VOLUME_STATUS_DELETED
}

// ValidateVolumeClaimOwnership verifies the durable owner of a service-scoped
// claim. Callers that make a decision from a locked claim must invoke this
// after acquiring the lock so a stale pre-lock snapshot cannot authorize a
// binding for a previous workload.
func ValidateVolumeClaimOwnership(claim *storagev1.VolumeClaim, workloadID, workloadType string) error {
	if claim == nil {
		return fmt.Errorf("volume claim is required")
	}
	if claim.GetBindingScope() != storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE {
		return nil
	}
	workloadID = strings.TrimSpace(workloadID)
	workloadType = strings.TrimSpace(workloadType)
	if workloadID == "" || workloadType == "" {
		return fmt.Errorf("volume claim binding workload id and workload type are required")
	}
	if claim.GetOwnerID() != workloadID || claim.GetOwnerType() != workloadType {
		return fmt.Errorf("volume claim %q/%q is owned by another workload", claim.GetNamespace(), claim.GetName())
	}
	return nil
}

func TransitionClaimStatus(current, next storagev1.VolumeStatus) error {
	if current == storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED {
		return fmt.Errorf("current volume claim status is required")
	}
	if next == storagev1.VolumeStatus_VOLUME_STATUS_UNSPECIFIED {
		return fmt.Errorf("next volume claim status is required")
	}
	if current == next {
		return nil
	}
	if IsTerminalClaimStatus(current) {
		return fmt.Errorf("volume claim status %s cannot transition to %s", current, next)
	}
	switch current {
	case storagev1.VolumeStatus_VOLUME_STATUS_PENDING:
		switch next {
		case storagev1.VolumeStatus_VOLUME_STATUS_BOUND, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING, storagev1.VolumeStatus_VOLUME_STATUS_DELETING, storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
			return nil
		}
	case storagev1.VolumeStatus_VOLUME_STATUS_BOUND:
		switch next {
		case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHING, storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING, storagev1.VolumeStatus_VOLUME_STATUS_DELETING, storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
			return nil
		}
	case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHING:
		switch next {
		case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING, storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
			return nil
		}
	case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:
		switch next {
		case storagev1.VolumeStatus_VOLUME_STATUS_BOUND, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING, storagev1.VolumeStatus_VOLUME_STATUS_DELETING, storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
			return nil
		}
	case storagev1.VolumeStatus_VOLUME_STATUS_RELEASING:
		switch next {
		case storagev1.VolumeStatus_VOLUME_STATUS_DELETED, storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
			return nil
		}
	case storagev1.VolumeStatus_VOLUME_STATUS_DELETING:
		if next == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			return nil
		}
	case storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
		switch next {
		case storagev1.VolumeStatus_VOLUME_STATUS_BOUND, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING, storagev1.VolumeStatus_VOLUME_STATUS_DELETING:
			return nil
		}
	}
	return fmt.Errorf("volume claim status cannot transition from %s to %s", current, next)
}
