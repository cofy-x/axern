package pgrun

import runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"

func parseRunStatus(value string) runv1.RunStatus {
	if n, ok := runv1.RunStatus_value[value]; ok {
		return runv1.RunStatus(n)
	}
	return runv1.RunStatus_RUN_STATUS_UNSPECIFIED
}
