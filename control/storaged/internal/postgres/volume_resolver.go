package postgres

import (
	"maps"
	"slices"
	"sort"
	"strings"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/proto"
)

func resolvedNodeVolume(req kernel.VolumeBindingReserve) *privatestoragev1.ResolvedNodeVolume {
	options := normalizeMountOptions(req.Mount)
	params := map[string]string{
		"namespace":   req.Namespace,
		"service_id":  req.WorkloadID,
		"volume_name": req.Claim.GetName(),
	}
	for key, value := range req.Class.GetParameters() {
		params[key] = value
	}
	for key, value := range req.Claim.GetParameters() {
		params[key] = value
	}
	return &privatestoragev1.ResolvedNodeVolume{
		ClaimID:              req.Claim.GetID(),
		BindingID:            req.AllocationID + "/" + req.Claim.GetName(),
		VolumeID:             req.Claim.GetName(),
		BackendHandle:        req.Claim.GetBackendHandle(),
		Backend:              req.Class.GetBackend(),
		AccessMode:           req.Claim.GetAccessMode(),
		Topology:             &storagev1.VolumeTopology{NodeID: req.NodeID},
		Target:               strings.TrimSpace(req.Mount.GetTarget()),
		Readonly:             req.Mount.GetReadonly(),
		Options:              options,
		Parameters:           params,
		ConsistencyProfile:   req.Class.GetConsistencyProfile(),
		RuntimeCompatibility: cloneRuntimeCompatibility(req.Class.GetRuntimeCompatibility()),
	}
}

func normalizeMountOptions(mount *privatestoragev1.WorkloadVolumeMount) []string {
	seen := map[string]struct{}{"rbind": {}}
	out := []string{"rbind"}
	for _, option := range mount.GetOptions() {
		option = strings.TrimSpace(option)
		if option == "" || option == "ro" || option == "rw" {
			continue
		}
		if _, ok := seen[option]; ok {
			continue
		}
		seen[option] = struct{}{}
		out = append(out, option)
	}
	sort.Strings(out[1:])
	if mount.GetReadonly() {
		out = append(out, "ro")
	} else {
		out = append(out, "rw")
	}
	return out
}

func resolvedNodeVolumesEqual(left, right *privatestoragev1.ResolvedNodeVolume) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.GetClaimID() == right.GetClaimID() &&
		left.GetBindingID() == right.GetBindingID() &&
		left.GetVolumeID() == right.GetVolumeID() &&
		left.GetBackendHandle() == right.GetBackendHandle() &&
		left.GetBackend() == right.GetBackend() &&
		left.GetAccessMode() == right.GetAccessMode() &&
		left.GetTopology().GetNodeID() == right.GetTopology().GetNodeID() &&
		left.GetTarget() == right.GetTarget() &&
		left.GetReadonly() == right.GetReadonly() &&
		slices.Equal(left.GetOptions(), right.GetOptions()) &&
		maps.Equal(left.GetParameters(), right.GetParameters()) &&
		left.GetConsistencyProfile() == right.GetConsistencyProfile() &&
		proto.Equal(left.GetRuntimeCompatibility(), right.GetRuntimeCompatibility())
}
