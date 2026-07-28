package parse

import (
	"fmt"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

const validRunStatuses = "queued, placed, starting, running, succeeded, failed, cancelled"

func RunStatuses(values []string) ([]runv1.RunStatus, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]runv1.RunStatus, 0, len(values))
	for _, value := range splitList(values) {
		switch normalizeToken(value) {
		case "queued":
			out = append(out, runv1.RunStatus_RUN_STATUS_QUEUED)
		case "placed":
			out = append(out, runv1.RunStatus_RUN_STATUS_PLACED)
		case "starting":
			out = append(out, runv1.RunStatus_RUN_STATUS_STARTING)
		case "running":
			out = append(out, runv1.RunStatus_RUN_STATUS_RUNNING)
		case "succeeded":
			out = append(out, runv1.RunStatus_RUN_STATUS_SUCCEEDED)
		case "failed":
			out = append(out, runv1.RunStatus_RUN_STATUS_FAILED)
		case "cancelled", "canceled":
			out = append(out, runv1.RunStatus_RUN_STATUS_CANCELLED)
		default:
			return nil, fmt.Errorf("invalid run status %q, want one of: %s", value, validRunStatuses)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
