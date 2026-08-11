package contract

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"google.golang.org/protobuf/proto"
)

func TestValidateEnforcementManifestRejectsCrossRuntimeAndMutableBackingState(t *testing.T) {
	valid := &apipb.AllocationEnforcementManifest{
		RuntimeName: "runsc", EphemeralStorageLimitBytes: 2048,
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
		"cross-runtime project":    func(m *apipb.AllocationEnforcementManifest) { m.RuncProjectID = 9 },
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
