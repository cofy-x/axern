package contract

import (
	"fmt"
	"path"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agentbundle"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

type AgentRuntimeProblem struct {
	Field   string
	Message string
}

func IsAgentLauncherKind(value domain.AgentLauncherKind) bool {
	switch value {
	case "", domain.AgentLauncherKindSandboxCommand, domain.AgentLauncherKindAgentImage:
		return true
	default:
		return false
	}
}

func ValidateAgentRuntimeSpec(runtime *domain.AgentRuntimeSpec) []AgentRuntimeProblem {
	if runtime == nil {
		return nil
	}
	var problems []AgentRuntimeProblem
	if runtime.Type != "" && !IsAgentRuntimeType(runtime.Type) {
		problems = append(problems, AgentRuntimeProblem{
			Field:   "type",
			Message: fmt.Sprintf("unsupported agent runtime type %q", runtime.Type),
		})
	}
	if len(runtime.Command) > 0 && len(runtime.Entrypoint) > 0 {
		problems = append(problems, AgentRuntimeProblem{
			Field:   "command",
			Message: "command and entrypoint are mutually exclusive",
		})
	}
	if len(runtime.Args) > 0 && len(runtime.Entrypoint) == 0 {
		problems = append(problems, AgentRuntimeProblem{Field: "args", Message: "args require entrypoint"})
	}
	if runtime.Type == domain.AgentRuntimeTypeAgentImage && strings.TrimSpace(runtime.Image) == "" {
		problems = append(problems, AgentRuntimeProblem{Field: "image", Message: "is required"})
	}
	if runtime.Type == domain.AgentRuntimeTypeAgentImage {
		if mountTarget := strings.TrimSpace(runtime.MountTarget); mountTarget != "" && !agentbundle.ValidMountTarget(mountTarget) {
			problems = append(problems, AgentRuntimeProblem{
				Field:   "mount_target",
				Message: "must be under /opt/axern/agents/<agent-name>",
			})
		}
		if binDir := strings.TrimSpace(runtime.BinDir); binDir != "" && !agentbundle.ValidBinDir(runtime.MountTarget, binDir) {
			problems = append(problems, AgentRuntimeProblem{Field: "bin_dir", Message: "must be <mount_target>/bin"})
		}
	}
	if runtime.Type == domain.AgentRuntimeTypeSandboxCommand && len(runtime.Command) == 0 && len(runtime.Entrypoint) == 0 {
		problems = append(problems, AgentRuntimeProblem{
			Field:   "command",
			Message: "sandbox-command runtime requires command or entrypoint",
		})
	}
	if runtime.Type == domain.AgentRuntimeTypeOracle && strings.TrimSpace(runtime.Image) != "" {
		problems = append(problems, AgentRuntimeProblem{Field: "image", Message: "must be empty"})
	}
	if runtime.TimeoutSec < 0 {
		problems = append(problems, AgentRuntimeProblem{
			Field:   "timeout_sec",
			Message: "must be greater than or equal to zero",
		})
	}
	if runtime.MaxTurns < 0 {
		problems = append(problems, AgentRuntimeProblem{Field: "max_turns", Message: "must be greater than or equal to zero"})
	}
	if runtime.IdleTimeoutSec < 0 {
		problems = append(problems, AgentRuntimeProblem{
			Field:   "idle_timeout_sec",
			Message: "must be greater than or equal to zero",
		})
	}
	if runtime.Artifacts != nil {
		for index, outputPath := range runtime.Artifacts.OutputPaths {
			if err := ValidateArtifactOutputPath(outputPath); err != nil {
				problems = append(problems, AgentRuntimeProblem{
					Field:   fmt.Sprintf("artifacts.output_paths[%d]", index),
					Message: err.Error(),
				})
			}
		}
	}
	return problems
}

func ValidateArtifactOutputPath(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("must not be empty")
	}
	clean := path.Clean(value)
	if clean == "/" {
		return fmt.Errorf("must not be the sandbox root")
	}
	if !path.IsAbs(clean) && (clean == ".." || strings.HasPrefix(clean, "../")) {
		return fmt.Errorf("must not escape the agent workdir")
	}
	return nil
}
