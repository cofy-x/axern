package runtime

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"google.golang.org/protobuf/proto"
)

func TestVerifyDurableEnforcementManifestRequiresExactImmutableContract(t *testing.T) {
	durable := &apipb.AllocationEnforcementManifest{
		RuntimeName: "runsc", MemoryLimitBytes: 1024, EphemeralStorageLimitBytes: 2048,
		CgroupPath: "/workload", RuntimeCgroupPath: "/workload/runtime",
		CgroupBootID: "boot-1", CgroupMountIdentity: "mount-1",
		CgroupParentInode: 10, CgroupLeafInode: 11, MemoryOomGroup: true,
		RunscOverlayArg:        "root:dir=/filestore/runsc,size=2048",
		FilestoreMountIdentity: "42:/dev/loop0:/filestore", BundlePath: "/containers/allocation",
		RunscBackingDirectory: "/filestore/runsc", RunscBackingDirectoryIdentity: "devino:v1:fe:10",
		CreatedAtUnixNano: 1,
	}
	if err := verifyDurableEnforcementManifest(durable, proto.Clone(durable).(*apipb.AllocationEnforcementManifest)); err != nil {
		t.Fatalf("equal manifests rejected: %v", err)
	}
	if err := verifyDurableEnforcementManifest(nil, durable); err == nil {
		t.Fatal("missing durable manifest accepted")
	}
	tampered := proto.Clone(durable).(*apipb.AllocationEnforcementManifest)
	tampered.RunscOverlayArg = "root:dir=/other,size=2048"
	if err := verifyDurableEnforcementManifest(durable, tampered); err == nil {
		t.Fatal("tampered launch contract accepted")
	}
}
