package agentbundle

import (
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	sharedagentbundle "github.com/cofy-x/axern/lib/go/agentbundle"
)

const MountRoot = sharedagentbundle.MountRoot

func MountTarget(agentName string) string {
	return sharedagentbundle.MountTarget(agentName)
}

func MountTargetForSpec(spec domain.AgentSpec) string {
	if spec.Runtime != nil && strings.TrimSpace(spec.Runtime.MountTarget) != "" {
		return spec.Runtime.MountTarget
	}
	return MountTarget(spec.Name)
}

func ImageMountTargetForSpec(spec domain.AgentSpec) string {
	return sharedagentbundle.ImageMountTarget(MountTargetForSpec(spec))
}

func BinDir(mountTarget string) string {
	return sharedagentbundle.BinDir(mountTarget)
}

func ValidMountTarget(target string) bool {
	return sharedagentbundle.ValidMountTarget(target)
}

func ValidBinDir(mountTarget string, binDir string) bool {
	return sharedagentbundle.ValidBinDir(mountTarget, binDir)
}
