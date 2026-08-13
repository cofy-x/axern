package agentbundle

import (
	"path"
	"strings"
)

const (
	MountRoot                = "/opt/axern/agents"
	ClaudeCodeMountTarget    = MountRoot + "/claude-code"
	ClaudeCodeABIMountTarget = "/__claude_code"
)

func MountTarget(agentName string) string {
	name := sanitizeMountName(agentName)
	if name == "" {
		name = "agent"
	}
	return MountRoot + "/" + name
}

// ImageMountTarget returns the actual OCI bind mount target for a public agent
// bundle path. Claude Code has a private short ABI path so its Bun executable
// can name the mount-local loader without growing PT_INTERP.
func ImageMountTarget(publicMountTarget string) string {
	if strings.TrimSpace(publicMountTarget) == ClaudeCodeMountTarget {
		return ClaudeCodeABIMountTarget
	}
	return strings.TrimSpace(publicMountTarget)
}

// ClaimedMountTargets returns every container path reserved by one image
// mount. Claude Code's single private ABI mount also reserves its public alias.
func ClaimedMountTargets(imageMountTarget string) []string {
	target := strings.TrimSpace(imageMountTarget)
	if target == ClaudeCodeABIMountTarget {
		return []string{ClaudeCodeABIMountTarget, ClaudeCodeMountTarget}
	}
	return []string{target}
}

func BinDir(mountTarget string) string {
	mountTarget = strings.TrimSpace(mountTarget)
	if !ValidMountTarget(mountTarget) {
		return ""
	}
	return path.Join(mountTarget, "bin")
}

func MountedBinary(mountTarget, binaryPath string) string {
	mountTarget = strings.TrimSpace(mountTarget)
	binaryPath = strings.TrimSpace(binaryPath)
	if !ValidMountTarget(mountTarget) || !ValidBinaryPath(binaryPath) {
		return ""
	}
	return path.Join(mountTarget, strings.TrimPrefix(binaryPath, "/"))
}

func ValidMountTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "." || target == "/" || strings.Contains(target, "\x00") {
		return false
	}
	if path.Clean(target) != target {
		return false
	}
	prefix := MountRoot + "/"
	if !strings.HasPrefix(target, prefix) {
		return false
	}
	name := strings.TrimPrefix(target, prefix)
	return name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

func ValidBinDir(mountTarget, binDir string) bool {
	mountTarget = strings.TrimSpace(mountTarget)
	binDir = strings.TrimSpace(binDir)
	return ValidMountTarget(mountTarget) && binDir == path.Join(mountTarget, "bin")
}

func ValidBinaryPath(binaryPath string) bool {
	binaryPath = strings.TrimSpace(binaryPath)
	if binaryPath == "" || strings.Contains(binaryPath, "\x00") || !strings.HasPrefix(binaryPath, "/") {
		return false
	}
	cleaned := path.Clean(binaryPath)
	return cleaned == binaryPath && cleaned != "/"
}

func sanitizeMountName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if ok {
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
