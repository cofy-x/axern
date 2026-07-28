package managedworker

import (
	"context"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/lib/go/agentprofile"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
)

func (w Worker) doctor(ctx context.Context, work *workerrolloutv1.WorkItem, leaseToken string) error {
	resolved, err := w.resolveAgentProfile(ctx, work, leaseToken)
	if err != nil {
		return err
	}
	checks := []*agentprofilev1.ProfileCheck{
		{
			Kind:    agentprofilev1.ProfileCheckKind_PROFILE_CHECK_KIND_CONFIGURATION,
			Status:  agentprofilev1.ProfileCheckStatus_PROFILE_CHECK_STATUS_PASS,
			Code:    "ok",
			Message: "profile configuration is valid",
		},
		{
			Kind:    agentprofilev1.ProfileCheckKind_PROFILE_CHECK_KIND_CREDENTIAL,
			Status:  agentprofilev1.ProfileCheckStatus_PROFILE_CHECK_STATUS_PASS,
			Code:    "ok",
			Message: "encrypted credential version resolved",
		},
		{
			Kind:    agentprofilev1.ProfileCheckKind_PROFILE_CHECK_KIND_WORKER_CAPABILITY,
			Status:  agentprofilev1.ProfileCheckStatus_PROFILE_CHECK_STATUS_PASS,
			Code:    "ok",
			Message: "probe is running from a rollout worker",
		},
	}
	result, probeErr := agentprofile.Probe(ctx, agentprofile.ProbeRequest{
		Profile: resolved.Runtime,
		Model:   work.GetProfileDoctor().GetModel(),
	})
	providerStatus := agentprofilev1.ProfileCheckStatus_PROFILE_CHECK_STATUS_PASS
	if probeErr != nil {
		providerStatus = agentprofilev1.ProfileCheckStatus_PROFILE_CHECK_STATUS_FAIL
	}
	checks = append(checks,
		&agentprofilev1.ProfileCheck{
			Kind:      agentprofilev1.ProfileCheckKind_PROFILE_CHECK_KIND_PROVIDER_AUTHENTICATION,
			Status:    providerStatus,
			Code:      defaultCheckCode(result.ErrorClass),
			Message:   result.Message,
			Retryable: result.Retryable,
			LatencyMs: result.LatencyMS,
		},
		&agentprofilev1.ProfileCheck{
			Kind:      agentprofilev1.ProfileCheckKind_PROFILE_CHECK_KIND_WIRE_API,
			Status:    providerStatus,
			Code:      defaultCheckCode(result.ErrorClass),
			Message:   result.Message,
			Retryable: result.Retryable,
			LatencyMs: result.LatencyMS,
		},
		&agentprofilev1.ProfileCheck{
			Kind:      agentprofilev1.ProfileCheckKind_PROFILE_CHECK_KIND_MODEL,
			Status:    providerStatus,
			Code:      defaultCheckCode(result.ErrorClass),
			Message:   result.Message,
			Retryable: result.Retryable,
			LatencyMs: result.LatencyMS,
		},
		&agentprofilev1.ProfileCheck{
			Kind:      agentprofilev1.ProfileCheckKind_PROFILE_CHECK_KIND_LATENCY,
			Status:    providerStatus,
			Code:      defaultCheckCode(result.ErrorClass),
			Message:   fmt.Sprintf("provider probe completed in %dms", result.LatencyMS),
			Retryable: result.Retryable,
			LatencyMs: result.LatencyMS,
		},
	)
	_, err = w.client.CompleteProfileDoctor(ctx, &workerrolloutv1.CompleteProfileDoctorRequest{
		WorkID:     work.GetID(),
		LeaseToken: leaseToken,
		Checks:     checks,
		Healthy:    probeErr == nil,
	})
	return err
}

func defaultCheckCode(value string) string {
	if strings.TrimSpace(value) == "" {
		return "ok"
	}
	return value
}
