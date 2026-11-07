package resources

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func conformanceRecoveryFixture() (*CgroupManager, *apipb.CgroupLease, *apipb.AllocationEnforcementManifest) {
	lease := &apipb.CgroupLease{
		CgroupID: "/node/conformance/owned", AllocationID: "ordinary-looking-id",
		OwnerKind: apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE,
		State:     apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED, AssignedAtUnixNano: 1,
		MemoryLimitBytes: 1024, MemoryRequestBytes: 1024, CapacityChargeBytes: 2048,
		CgroupBootID: "boot", CgroupMountIdentity: "mount", CgroupParentInode: 11, CgroupLeafInode: 12,
	}
	manifest := &apipb.AllocationEnforcementManifest{
		CgroupPath: lease.CgroupID, RuntimeCgroupPath: lease.CgroupID + "/workload", MemoryLimitBytes: lease.MemoryLimitBytes,
		CgroupBootID: lease.CgroupBootID, CgroupMountIdentity: lease.CgroupMountIdentity,
		CgroupParentInode: lease.CgroupParentInode, CgroupLeafInode: lease.CgroupLeafInode,
	}
	manager := &CgroupManager{conformanceRoot: "/node/conformance", leases: cmap.New[*apipb.CgroupLease]()}
	manager.leases.Set(lease.CgroupID, lease)
	return manager, lease, manifest
}

func TestConformanceRecoveryUsesTypedDurableOwnership(t *testing.T) {
	manager, lease, manifest := conformanceRecoveryFixture()
	before := proto.Clone(lease)
	require.NoError(t, manager.VerifyRuntimeConformanceBinding(lease.AllocationID, manifest))
	require.True(t, proto.Equal(before, lease), "verification must not repair or mutate ownership")
	lease.State = apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING
	lease.RetiringAtUnixNano = 2
	require.NoError(t, manager.VerifyRuntimeConformanceBinding(lease.AllocationID, manifest))
}

func TestConformanceRecoveryRejectsUnownedOrDifferentDomain(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*CgroupManager, *apipb.CgroupLease, *apipb.AllocationEnforcementManifest)
	}{
		{"missing lease", func(m *CgroupManager, l *apipb.CgroupLease, _ *apipb.AllocationEnforcementManifest) {
			m.leases.Remove(l.CgroupID)
		}},
		{"workload with selftest name", func(_ *CgroupManager, l *apipb.CgroupLease, _ *apipb.AllocationEnforcementManifest) {
			l.OwnerKind = apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD
			l.AllocationID = "capability-selftest-runsc-ephemeral-storage-allocation"
			l.CapacityChargeBytes = l.MemoryRequestBytes
		}},
		{"untyped lease", func(_ *CgroupManager, l *apipb.CgroupLease, _ *apipb.AllocationEnforcementManifest) {
			l.OwnerKind = apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_UNSPECIFIED
		}},
		{"idle lease", func(_ *CgroupManager, l *apipb.CgroupLease, _ *apipb.AllocationEnforcementManifest) {
			l.State = apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE
		}},
		{"other allocation", func(_ *CgroupManager, l *apipb.CgroupLease, _ *apipb.AllocationEnforcementManifest) {
			l.AllocationID = "another-owner"
		}},
		{"wrong root", func(m *CgroupManager, _ *apipb.CgroupLease, _ *apipb.AllocationEnforcementManifest) {
			m.conformanceRoot = "/another/conformance"
		}},
		{"wrong leaf", func(_ *CgroupManager, _ *apipb.CgroupLease, p *apipb.AllocationEnforcementManifest) {
			p.RuntimeCgroupPath += "/child"
		}},
		{"noncanonical path", func(_ *CgroupManager, _ *apipb.CgroupLease, p *apipb.AllocationEnforcementManifest) {
			p.CgroupPath += "/../owned"
		}},
		{"changed limit", func(_ *CgroupManager, _ *apipb.CgroupLease, p *apipb.AllocationEnforcementManifest) {
			p.MemoryLimitBytes++
		}},
		{"changed boot", func(_ *CgroupManager, _ *apipb.CgroupLease, p *apipb.AllocationEnforcementManifest) {
			p.CgroupBootID = "other"
		}},
		{"changed mount", func(_ *CgroupManager, _ *apipb.CgroupLease, p *apipb.AllocationEnforcementManifest) {
			p.CgroupMountIdentity = "other"
		}},
		{"changed parent", func(_ *CgroupManager, _ *apipb.CgroupLease, p *apipb.AllocationEnforcementManifest) {
			p.CgroupParentInode++
		}},
		{"changed leaf", func(_ *CgroupManager, _ *apipb.CgroupLease, p *apipb.AllocationEnforcementManifest) {
			p.CgroupLeafInode++
		}},
		{"partial ledger identity", func(_ *CgroupManager, l *apipb.CgroupLease, p *apipb.AllocationEnforcementManifest) {
			l.CgroupLeafInode = 0
			p.CgroupLeafInode = 0
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, lease, manifest := conformanceRecoveryFixture()
			id := lease.AllocationID
			tc.mutate(manager, lease, manifest)
			require.Error(t, manager.VerifyRuntimeConformanceBinding(id, manifest))
		})
	}
}

func TestConformanceRecoveryBindingDoesNotInventMemoryIdentity(t *testing.T) {
	manager, lease, manifest := conformanceRecoveryFixture()
	lease.MemoryRequestBytes, lease.MemoryLimitBytes = 0, 0
	lease.CgroupBootID, lease.CgroupMountIdentity = "", ""
	lease.CgroupParentInode, lease.CgroupLeafInode = 0, 0
	manifest.MemoryLimitBytes = 0
	manifest.CgroupBootID, manifest.CgroupMountIdentity = "", ""
	manifest.CgroupParentInode, manifest.CgroupLeafInode = 0, 0
	require.NoError(t, manager.VerifyRuntimeConformanceBinding(lease.AllocationID, manifest))
	// This proves typed ownership only. It cannot fabricate missing identity
	// for a memoryless storage probe or authorize runtime continuation.
	require.Zero(t, manifest.GetCgroupLeafInode())
	manifest.CgroupLeafInode = 12
	require.Error(t, manager.VerifyRuntimeConformanceBinding(lease.AllocationID, manifest))
}

func TestConformanceNameDoesNotAuthorizeWorkloadCleanup(t *testing.T) {
	manager, lease, manifest := conformanceRecoveryFixture()
	lease.AllocationID = "capability-selftest-runsc-ephemeral-storage-allocation"
	lease.OwnerKind = apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD
	lease.CapacityChargeBytes = lease.MemoryRequestBytes
	require.ErrorContains(t, manager.VerifyRuntimeConformanceBinding(lease.AllocationID, manifest), "typed cgroup lease")
}
