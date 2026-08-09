package allocation

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestStartRequestDigestCanonicalizesTelemetryAndCapabilityProofs(t *testing.T) {
	request := testDigestStartRequest()
	request.TraceID = "trace-one"
	request.CapabilityDependencies[0].SelectedObservation = &capabilityv1.CapabilityObservationProof{ObservationID: "placement-proof-one"}
	first, err := StartRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}

	retry := proto.Clone(request).(*apipb.StartRequest)
	retry.TraceID = "trace-two"
	retry.CapabilityDependencies[0].SelectedObservation = &capabilityv1.CapabilityObservationProof{ObservationID: "placement-proof-two"}
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
		"command": func(candidate *apipb.StartRequest) { candidate.RuntimeTemplate.Command = []string{"/bin/false"} },
		"memory":  func(candidate *apipb.StartRequest) { candidate.Resources.Limits.MemoryBytes++ },
		"runtime": func(candidate *apipb.StartRequest) { candidate.RuntimeTemplate.Sandbox = "runc" },
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
	request.CapabilityDependencies[0].LossPolicy = capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP
	if _, err := StartRequestDigest(request); err == nil {
		t.Fatal("StartRequestDigest() accepted a non-catalog loss policy")
	}
}

func testDigestStartRequest() *apipb.StartRequest {
	return &apipb.StartRequest{
		ContainerID:       "allocation-digest",
		AllocationAttempt: 7,
		RuntimeTemplate: &apipb.RuntimeTemplate{
			ID: "runtime-digest", Sandbox: "runsc",
			Rootfs:  &apipb.RootfsConfig{Readonly: true, Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: "/rootfs"}},
			Command: []string{"/bin/true"},
		},
		Resources: &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 64 << 20}},
		CapabilityDependencies: []*capabilityv1.CapabilityDependency{{
			Key:        &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{Platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING}},
			LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE,
		}},
		ExtensionCapabilityRequirements: []*capabilityv1.ExtensionCapabilityRequirement{
			{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/zeta", Value: "two"}},
			{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/alpha", Value: "one"}},
		},
	}
}
