package storage

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	"google.golang.org/protobuf/proto"
)

func (m *Manager) Get(allocationID, bindingID string) (*runtimevolumev1.PublishedVolume, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.published[strings.TrimSpace(allocationID)] {
		if item.GetBindingID() == strings.TrimSpace(bindingID) {
			return clonePublished(item), true
		}
	}
	return nil, false
}

func (m *Manager) List(allocationID string) []*runtimevolumev1.PublishedVolume {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	allocationID = strings.TrimSpace(allocationID)
	if allocationID != "" {
		return clonePublishedSlice(m.published[allocationID])
	}
	out := []*runtimevolumev1.PublishedVolume{}
	for _, items := range m.published {
		out = append(out, clonePublishedSlice(items)...)
	}
	return out
}

func upsertPublished(items map[string][]*runtimevolumev1.PublishedVolume, allocationID string, item *runtimevolumev1.PublishedVolume) {
	existing := items[allocationID]
	next := clonePublished(item)
	for idx, current := range existing {
		if current.GetBindingID() == item.GetBindingID() {
			existing[idx] = next
			items[allocationID] = existing
			return
		}
	}
	items[allocationID] = append(existing, next)
}

func removePublished(items map[string][]*runtimevolumev1.PublishedVolume, allocationID, bindingID string) {
	existing := items[allocationID]
	retained := existing[:0]
	for _, item := range existing {
		if item == nil || item.GetBindingID() == bindingID {
			continue
		}
		retained = append(retained, item)
	}
	if len(retained) == 0 {
		delete(items, allocationID)
		return
	}
	items[allocationID] = retained
}

func removePublishedIfEqual(items map[string][]*runtimevolumev1.PublishedVolume, allocationID string, expected *runtimevolumev1.PublishedVolume) bool {
	if expected == nil {
		return false
	}
	existing := items[allocationID]
	match := -1
	for idx, item := range existing {
		if item == nil {
			continue
		}
		if item.GetBindingID() == expected.GetBindingID() && proto.Equal(item, expected) {
			match = idx
			break
		}
	}
	if match < 0 {
		return false
	}
	retained := existing[:0]
	for idx, item := range existing {
		if item == nil {
			continue
		}
		if idx == match {
			continue
		}
		retained = append(retained, item)
	}
	if len(retained) == 0 {
		delete(items, allocationID)
	} else {
		items[allocationID] = retained
	}
	return true
}

func countPublished(items map[string][]*runtimevolumev1.PublishedVolume) int {
	var count int
	for _, volumes := range items {
		for _, volume := range volumes {
			if volume != nil {
				count++
			}
		}
	}
	return count
}

func sortedPublishedAllocationIDs(items map[string][]*runtimevolumev1.PublishedVolume) []string {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func clonePublished(in *runtimevolumev1.PublishedVolume) *runtimevolumev1.PublishedVolume {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*runtimevolumev1.PublishedVolume)
}

func clonePublishedSlice(in []*runtimevolumev1.PublishedVolume) []*runtimevolumev1.PublishedVolume {
	if len(in) == 0 {
		return nil
	}
	out := make([]*runtimevolumev1.PublishedVolume, 0, len(in))
	for _, item := range in {
		out = append(out, clonePublished(item))
	}
	return out
}

func clonePublishedMap(in map[string][]*runtimevolumev1.PublishedVolume) map[string][]*runtimevolumev1.PublishedVolume {
	out := make(map[string][]*runtimevolumev1.PublishedVolume, len(in))
	for allocationID, items := range in {
		out[allocationID] = clonePublishedSlice(items)
	}
	return out
}

func validatePublishedRecord(allocationID string, item *runtimevolumev1.PublishedVolume) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return fmt.Errorf("published volume allocation id is required")
	}
	if item == nil {
		return fmt.Errorf("published volume for allocation %s is required", allocationID)
	}
	bindingID := strings.TrimSpace(item.GetBindingID())
	if bindingID == "" {
		return fmt.Errorf("published volume for allocation %s requires binding id", allocationID)
	}
	item.BindingID = bindingID
	claimID := strings.TrimSpace(item.GetClaimID())
	if claimID == "" {
		return fmt.Errorf("published volume %s/%s requires claim id", allocationID, bindingID)
	}
	item.ClaimID = claimID
	if item.GetBackend() == storagev1.VolumeBackend_VOLUME_BACKEND_UNSPECIFIED {
		return fmt.Errorf("published volume %s/%s requires backend", allocationID, bindingID)
	}
	hostPath := strings.TrimSpace(item.GetHostPath())
	if hostPath == "" || !filepath.IsAbs(hostPath) {
		return fmt.Errorf("published volume %s/%s requires absolute host path", allocationID, bindingID)
	}
	item.HostPath = hostPath
	target := cleanContainerTarget(item.GetTarget())
	if target == "" {
		return fmt.Errorf("published volume %s/%s requires an absolute container target below /", allocationID, bindingID)
	}
	item.Target = target
	for _, option := range item.GetOptions() {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		if !allowedMountOption(option) {
			return fmt.Errorf("published volume %s/%s option %q is not supported", allocationID, bindingID, option)
		}
		if item.GetReadonly() && option == "rw" {
			return fmt.Errorf("published volume %s/%s readonly volume must not include rw option", allocationID, bindingID)
		}
		if !item.GetReadonly() && option == "ro" {
			return fmt.Errorf("published volume %s/%s writable volume must not include ro option", allocationID, bindingID)
		}
	}
	item.Options = normalizeMountOptions(item.GetOptions(), item.GetReadonly())
	return nil
}
