package contract

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"google.golang.org/protobuf/proto"
)

func TestValidateEnforcementManifestRejectsCrossRuntimeAndMutableBackingState(t *testing.T) {
	valid := &apipb.AllocationEnforcementManifest{
		EphemeralStorageLimitBytes:    2048,
		FilestoreMountIdentity:        "42:/dev/loop0:/filestore",
		RunscOverlayArg:               "root:dir=/filestore/runsc,size=2048",
		RunscBackingDirectory:         "/filestore/runsc",
		RunscBackingDirectoryIdentity: "devino:v1:fe:10",
		BundlePath:                    "/containers/allocation", CreatedAtUnixNano: 1,
	}
	if err := ValidateEnforcementManifest(valid, valid.GetBundlePath()); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	tests := map[string]func(*apipb.AllocationEnforcementManifest){
		"relative bundle": func(m *apipb.AllocationEnforcementManifest) { m.BundlePath = "relative" },
		"overlay limit mismatch": func(m *apipb.AllocationEnforcementManifest) {
			m.RunscOverlayArg = "root:dir=/filestore/runsc,size=4096"
		},
		"missing backing identity": func(m *apipb.AllocationEnforcementManifest) { m.RunscBackingDirectoryIdentity = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			manifest := proto.Clone(valid).(*apipb.AllocationEnforcementManifest)
			mutate(manifest)
			if err := ValidateEnforcementManifest(manifest, ""); err == nil {
				t.Fatal("invalid enforcement manifest was accepted")
			}
		})
	}
	memory := proto.Clone(valid).(*apipb.AllocationEnforcementManifest)
	memory.MemoryLimitBytes = 1024
	if err := ValidateEnforcementManifest(memory, ""); err == nil {
		t.Fatal("memory manifest without cgroup identities was accepted")
	}
	memory.CgroupPath = "/sandbox/allocation"
	memory.RuntimeCgroupPath = "/sandbox/allocation/runtime"
	memory.CgroupBootID = "boot-1"
	memory.CgroupMountIdentity = "mount-1"
	memory.CgroupParentInode = 10
	memory.CgroupLeafInode = 11
	memory.MemoryOomGroup = true
	if err := ValidateEnforcementManifest(memory, ""); err != nil {
		t.Fatalf("valid memory enforcement manifest rejected: %v", err)
	}
}

func TestValidateTaskEnforcementManifest(t *testing.T) {
	valid := &apipb.AllocationEnforcementManifest{
		BundlePath: "/containers/allocation", CreatedAtUnixNano: 1,
		GuestTaskLimit: 256, HostTaskLimit: 1024,
		CgroupPath: "/sandbox/allocation", RuntimeCgroupPath: "/sandbox/allocation/workload",
	}
	if err := ValidateEnforcementManifest(valid, ""); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*apipb.AllocationEnforcementManifest){
		"partial pair":   func(m *apipb.AllocationEnforcementManifest) { m.HostTaskLimit = 0 },
		"negative pair":  func(m *apipb.AllocationEnforcementManifest) { m.GuestTaskLimit = -1 },
		"unbounded pair": func(m *apipb.AllocationEnforcementManifest) { m.HostTaskLimit = 65537 },
		"missing parent": func(m *apipb.AllocationEnforcementManifest) { m.CgroupPath = "" },
		"unrelated leaf": func(m *apipb.AllocationEnforcementManifest) { m.RuntimeCgroupPath = "/other/workload" },
		"nested leaf":    func(m *apipb.AllocationEnforcementManifest) { m.RuntimeCgroupPath += "/nested" },
	} {
		t.Run(name, func(t *testing.T) {
			m := proto.Clone(valid).(*apipb.AllocationEnforcementManifest)
			mutate(m)
			if err := ValidateEnforcementManifest(m, ""); err == nil {
				t.Fatal("invalid task policy accepted")
			}
		})
	}
}
