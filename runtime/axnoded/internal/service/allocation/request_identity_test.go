package allocation

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestStartRequestDigestCanonicalizesTelemetryAndRequirementOrder(t *testing.T) {
	request := testDigestStartRequest()
	request.TraceID = "trace-one"
	first, err := StartRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}

	retry := proto.Clone(request).(*apipb.StartRequest)
	retry.TraceID = "trace-two"
	retry.ExtensionCapabilityRequirements[0], retry.ExtensionCapabilityRequirements[1] = retry.ExtensionCapabilityRequirements[1], retry.ExtensionCapabilityRequirements[0]
	second, err := StartRequestDigest(retry)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("non-behavioral retry metadata changed digest: %q != %q", first, second)
	}
}

func TestStartRequestDigestChangesWithSandboxContract(t *testing.T) {
	request := testDigestStartRequest()
	baseline, err := StartRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*apipb.StartRequest){
		"command": func(candidate *apipb.StartRequest) { candidate.EnvironmentTemplate.Argv = []string{"/bin/false"} },
		"memory":  func(candidate *apipb.StartRequest) { candidate.Resources.Limits.MemoryBytes++ },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := proto.Clone(request).(*apipb.StartRequest)
			mutate(candidate)
			got, digestErr := StartRequestDigest(candidate)
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			if got == baseline {
				t.Fatalf("behavioral change %q retained digest %q", name, got)
			}
		})
	}
}

func TestStartRequestDigestRejectsCatalogPolicyMismatch(t *testing.T) {
	request := testDigestStartRequest()
	request.CapabilityRequirements[0].LossPolicy = capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP
	if _, err := StartRequestDigest(request); err == nil {
		t.Fatal("StartRequestDigest() accepted a non-catalog loss policy")
	}
}

func testDigestStartRequest() *apipb.StartRequest {
	return &apipb.StartRequest{
		AllocationID: "allocation-digest",
		EnvironmentTemplate: &apipb.EnvironmentTemplate{
			ID:     "runtime-digest",
			Rootfs: &apipb.RootfsConfig{Readonly: true, Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: "/rootfs"}},
			Argv:   []string{"/bin/true"},
		},
		Resources: &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 64 << 20}},
		CapabilityRequirements: []*capabilityv1.CapabilityRequirement{{
			Key:        &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{Platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING}},
			LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE,
		}},
		ExtensionCapabilityRequirements: []*capabilityv1.ExtensionCapabilityRequirement{
			{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/zeta", Value: "two"}},
			{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/alpha", Value: "one"}},
		},
	}
}
