package envelopepolicy

import (
	"strings"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func EligibleForStaticEnvelope(request *apipb.StartRequest, runtimeName string) bool {
	if request == nil || request.RuntimeTemplate == nil {
		return false
	}
	if request.RuntimeTemplate.Sandbox != runtimeName {
		return false
	}
	if request.CkptDir != "" || request.Network != "" || request.Stdout != "" || request.Stderr != "" {
		return false
	}
	if len(request.UserEnvs) > 0 || len(request.Mounts) > 0 || len(request.ImageMounts) > 0 || request.GetResources() != nil {
		return false
	}
	return strings.TrimSpace(request.ExtraConfig) == ""
}
