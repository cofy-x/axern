package service

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
)

// verifyMemoryPIDRoles is a read-only, /proc-based sampling check. It avoids
// spawning one runtime CLI process per allocation every inventory interval;
// the independent 60-second enforcement audit still executes the complete
// runtime verifier and owns fail-stop decisions.
func (h *sandboxService) verifyMemoryPIDRoles(allocationID, runtimeName, workloadPath string, runtimePID int) error {
	if strings.TrimSpace(allocationID) == "" || strings.TrimSpace(workloadPath) == "" || runtimePID <= 0 {
		return fmt.Errorf("allocation, workload cgroup, and runtime PID are required")
	}
	switch runtimeName {
	case config.RuntimeNameRunc:
		return hostlinux.VerifyRuncCgroupProcessTree(workloadPath, runtimePID)
	case config.RuntimeNameRunsc:
		runtimeConfig, ok := h.config.PluginConfig.RuntimeConfig.Runtimes[config.RuntimeNameRunsc]
		if !ok || strings.TrimSpace(runtimeConfig.Binary) == "" {
			return fmt.Errorf("configured runsc binary is unavailable")
		}
		return hostlinux.VerifyRunscCgroupProcesses(workloadPath, runtimePID, runtimeConfig.Binary)
	default:
		return fmt.Errorf("runtime %q does not have a memory PID-role verifier", runtimeName)
	}
}
