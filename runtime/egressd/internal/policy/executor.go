package policy

import (
	"context"

	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
)

type EnforcementHealth struct {
	DNSPolicyReady    bool
	StrictEgressReady bool
	Revision          int64
	Reason            string
}

// Executor atomically reconciles the host dataplane to an exact desired set.
// It is intentionally set-oriented so persistence failure can restore the
// prior generation without leaving a partially applied allocation policy.
type Executor interface {
	Reconcile(context.Context, []*runtimeegressv1.PreparedEgressPolicy) error
	Health(context.Context) EnforcementHealth
}

type unavailableExecutor struct{}

func (unavailableExecutor) Reconcile(context.Context, []*runtimeegressv1.PreparedEgressPolicy) error {
	return nil
}
func (unavailableExecutor) Health(context.Context) EnforcementHealth {
	return EnforcementHealth{Reason: "host enforcement executor is not configured"}
}
