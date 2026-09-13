package agentimage

import (
	"path"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

const MountRoot = "/opt/axern/agents"

func MountTarget(agentName string) string {
	name := sanitizeMountName(agentName)
	if name == "" {
		name = "agent"
	}
	return MountRoot + "/" + name
}

func MountTargetForSpec(spec domain.AgentSpec) string {
	if spec.Runtime != nil && strings.TrimSpace(spec.Runtime.MountTarget) != "" {
		return spec.Runtime.MountTarget
	}
	return MountTarget(spec.Name)
}

func BinDir(mountTarget string) string {
	if !ValidMountTarget(mountTarget) {
		return ""
	}
	return path.Join(strings.TrimSpace(mountTarget), "bin")
}

func ValidMountTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "." || target == "/" || strings.Contains(target, "\x00") || path.Clean(target) != target {
		return false
	}
	name := strings.TrimPrefix(target, MountRoot+"/")
	return name != target && name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

func ValidBinDir(mountTarget string, binDir string) bool {
	return ValidMountTarget(mountTarget) && strings.TrimSpace(binDir) == path.Join(strings.TrimSpace(mountTarget), "bin")
}

func sanitizeMountName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
