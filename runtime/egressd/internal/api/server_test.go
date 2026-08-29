package api

import (
	"context"
	"testing"

	"github.com/cofy-x/axern/runtime/egressd/internal/policy"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerLifecycle(t *testing.T) {
	manager, err := policy.NewManager(nil)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(manager)
	prepared, err := server.PreparePolicy(context.Background(), &runtimeegressv1.PreparePolicyRequest{
		AllocationID:      "alloc-1",
		Attempt:           1,
		SandboxIp:         "10.0.0.8",
		Policy:            dnsDeny("Example.COM."),
		ExecutionRevision: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.GetAlreadyPrepared() || prepared.GetPolicy().GetPolicyDigest() == "" {
		t.Fatalf("unexpected prepare response: %#v", prepared)
	}
	got, err := server.GetPolicy(context.Background(), &runtimeegressv1.GetPolicyRequest{AllocationID: "alloc-1", Attempt: 1})
	if err != nil || got.GetPolicy().GetSandboxIp() != "10.0.0.8" {
		t.Fatalf("GetPolicy = (%#v, %v)", got, err)
	}
	listed, err := server.ListPolicies(context.Background(), &runtimeegressv1.ListPoliciesRequest{})
	if err != nil || len(listed.GetPolicies()) != 1 {
		t.Fatalf("ListPolicies = (%#v, %v)", listed, err)
	}
	health, err := server.GetEgressManagerHealth(context.Background(), &runtimeegressv1.EgressManagerHealthRequest{})
	if err != nil || health.GetHealth().GetPreparedPolicyCount() != 1 {
		t.Fatalf("GetEgressManagerHealth = (%#v, %v)", health, err)
	}
	deleted, err := server.DeletePolicy(context.Background(), &runtimeegressv1.DeletePolicyRequest{AllocationID: "alloc-1", Attempt: 1})
	if err != nil || !deleted.GetDeleted() {
		t.Fatalf("DeletePolicy = (%#v, %v)", deleted, err)
	}
}

func TestServerMapsValidationAndFencingStatus(t *testing.T) {
	manager, err := policy.NewManager(nil)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(manager)
	_, err = server.PreparePolicy(context.Background(), &runtimeegressv1.PreparePolicyRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("validation status = %s, want InvalidArgument: %v", status.Code(err), err)
	}
	request := &runtimeegressv1.PreparePolicyRequest{AllocationID: "alloc", Attempt: 2, SandboxIp: "10.0.0.8", Policy: dnsDeny("example.com"), ExecutionRevision: 1}
	if _, err := server.PreparePolicy(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	request.Attempt = 1
	request.SandboxIp = "10.0.0.9"
	_, err = server.PreparePolicy(context.Background(), request)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("fencing status = %s, want FailedPrecondition: %v", status.Code(err), err)
	}
	_, err = server.GetPolicy(context.Background(), &runtimeegressv1.GetPolicyRequest{AllocationID: "missing", Attempt: 1})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing status = %s, want NotFound: %v", status.Code(err), err)
	}
}

func dnsDeny(domains ...string) *commonv1.NetworkEgressPolicy {
	return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: domains}}}
}
