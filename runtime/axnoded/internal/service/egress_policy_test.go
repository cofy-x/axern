package service

import (
	"context"
	"testing"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/egress"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type reconcileEgressFake struct {
	egress.Manager
	health *runtimeegressv1.EgressManagerHealth
	record *runtimeegressv1.PreparedEgressPolicy
	err    error
}

func (f *reconcileEgressFake) Health(context.Context) (*runtimeegressv1.EgressManagerHealth, error) {
	return f.health, nil
}

func (f *reconcileEgressFake) Get(_ context.Context, id string) (*runtimeegressv1.PreparedEgressPolicy, error) {
	if id != "policy-allocation" {
		return nil, status.Error(codes.NotFound, "wrong allocation identity")
	}
	return f.record, f.err
}

func TestEgressReconcileRoutesToPolicyOwner(t *testing.T) {
	for _, platform := range []capabilityv1.PlatformCapability{
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT,
	} {
		t.Run(platform.String(), func(t *testing.T) {
			// No OCI handler or container is installed: neither owns this proof.
			s := newTestService(t, nil)
			dependency := &capabilityv1.CapabilityRequirement{Key: capabilitycontract.PlatformKey(platform), LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP}
			policy := &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{}}}
			if platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT {
				policy.Policy = &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{}}
			}
			fake := &reconcileEgressFake{
				health: &runtimeegressv1.EgressManagerHealth{Status: runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_OK, DnsPolicySelfTestOk: true, StrictEgressSelfTestOk: true},
				record: &runtimeegressv1.PreparedEgressPolicy{AllocationID: "policy-allocation", SandboxIp: "192.0.2.10", Policy: policy},
			}
			s.egressClient = fake
			check := func(want contract.CapabilityVerificationState) {
				t.Helper()
				got := verifyActiveEgressPolicy(context.Background(), fake, "policy-allocation", "192.0.2.10", allocationNetworkPolicyMode([]*capabilityv1.CapabilityRequirement{dependency}))
				if got.State != want {
					t.Fatalf("verification = %+v, want %v", got, want)
				}
			}
			check(contract.CapabilityVerificationVerified)
			fake.record.AllocationID = "another-allocation"
			check(contract.CapabilityVerificationLost)
			fake.record.AllocationID = "policy-allocation"
			fake.record.SandboxIp = "192.0.2.11"
			check(contract.CapabilityVerificationLost)
			fake.record.SandboxIp = "192.0.2.10"
			fake.record.Policy = &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{}}}
			if platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT {
				check(contract.CapabilityVerificationLost)
			}
			fake.record.Policy = policy
			fake.err = status.Error(codes.Unavailable, "transport unavailable")
			check(contract.CapabilityVerificationInconclusive)
			fake.err = status.Error(codes.NotFound, "record absent")
			check(contract.CapabilityVerificationLost)
			fake.err = status.Error(codes.FailedPrecondition, "attempt fenced")
			check(contract.CapabilityVerificationLost)
			fake.err = nil
			fake.health.StrictEgressSelfTestOk = false
			fake.health.DnsPolicySelfTestOk = false
			check(contract.CapabilityVerificationLost)
		})
	}
}
