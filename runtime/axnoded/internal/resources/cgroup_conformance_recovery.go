package resources

import (
	"fmt"
	"path/filepath"
	"strings"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
)

// VerifyRuntimeConformanceBinding checks only the durable resource ownership
// of node-owned certification. It never admits an Allocation or proves that a
// sandbox has stopped. Runtime deletion must still verify the kernel domain.
func (c *CgroupManager) VerifyRuntimeConformanceBinding(allocationID string, manifest *apipb.AllocationEnforcementManifest) error {
	if c == nil || allocationID == "" || strings.TrimSpace(allocationID) != allocationID || manifest == nil {
		return fmt.Errorf("runtime conformance cleanup requires exact allocation and manifest")
	}
	c.Lock()
	defer c.Unlock()
	path := manifest.GetCgroupPath()
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.TrimSpace(path) != path ||
		c.conformanceRoot == "" || filepath.Dir(path) != c.conformanceRoot ||
		manifest.GetRuntimeCgroupPath() != cgroup.WorkloadGroup(path, cgroup.CgroupModeV2) {
		return fmt.Errorf("runtime conformance cgroup is outside its owned domain")
	}
	lease, ok := c.leases.Get(path)
	if !ok || lease == nil || lease.GetCgroupID() != path || lease.GetAllocationID() != allocationID ||
		lease.GetOwnerKind() != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE {
		return fmt.Errorf("runtime conformance cleanup requires its exact typed cgroup lease")
	}
	if err := validateCgroupLease(lease); err != nil {
		return fmt.Errorf("invalid runtime conformance ownership: %w", err)
	}
	if lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED &&
		lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING {
		return fmt.Errorf("runtime conformance cgroup is not owned")
	}
	if lease.GetMemoryLimitBytes() != manifest.GetMemoryLimitBytes() ||
		lease.GetCgroupBootID() != manifest.GetCgroupBootID() ||
		lease.GetCgroupMountIdentity() != manifest.GetCgroupMountIdentity() ||
		lease.GetCgroupParentInode() != manifest.GetCgroupParentInode() ||
		lease.GetCgroupLeafInode() != manifest.GetCgroupLeafInode() {
		return fmt.Errorf("runtime conformance projection differs from its durable cgroup lease")
	}
	return nil
}
